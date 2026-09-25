package agentbundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

// Platform 是安装脚本支持的 Agent 平台。arm 使用 GOARM=6，兼容 armv6 与 armv7。
type Platform struct{ OS, Arch, GOARM string }

var Platforms = []Platform{
	{"linux", "amd64", ""}, {"linux", "arm64", ""}, {"linux", "arm", "6"},
	{"windows", "amd64", ""}, {"windows", "386", ""},
	{"darwin", "amd64", ""}, {"darwin", "arm64", ""},
}

const maxBundleFileSize = 64 << 20

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

type Manifest struct {
	PanelVersion string            `json:"panelVersion"`
	AgentVersion string            `json:"agentVersion"`
	Files        map[string]string `json:"files"`
}

func FileName(p Platform) string {
	name := "agent-" + p.OS + "-" + p.Arch
	if p.OS == "windows" {
		name += ".exe"
	}
	return name
}

func ValidPlatform(goos, goarch string) bool {
	for _, p := range Platforms {
		if p.OS == goos && p.Arch == goarch {
			return true
		}
	}
	return false
}

// Create 从同一源码构建完整的 Agent 包，供 Release 和本地二进制部署共用。
func Create(ctx context.Context, root, output, panelVersion, agentVersion string) error {
	if !versionPattern.MatchString(panelVersion) || !versionPattern.MatchString(agentVersion) {
		return fmt.Errorf("面板或 Agent 版本号无效")
	}
	work, err := os.MkdirTemp("", "dfpanel-agent-bundle-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	manifest := Manifest{PanelVersion: panelVersion, AgentVersion: agentVersion, Files: make(map[string]string)}
	for _, p := range Platforms {
		name := FileName(p)
		path := filepath.Join(work, name)
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-X dfpanel/internal/agent.Version="+agentVersion, "-o", path, "./cmd/agent")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+p.OS, "GOARCH="+p.Arch, "GOARM="+p.GOARM)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("构建 %s/%s 失败：%w：%s", p.OS, p.Arch, err, out)
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		manifest.Files[name] = sum
	}
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	f, err := os.Create(output + ".tmp")
	if err != nil {
		return err
	}
	defer os.Remove(output + ".tmp")
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, p := range Platforms {
		name := FileName(p)
		data, err := os.ReadFile(filepath.Join(work, name))
		if err != nil {
			f.Close()
			return err
		}
		if err := writeTarFile(tw, name, data, 0o755); err != nil {
			f.Close()
			return err
		}
	}
	if err := writeTarFile(tw, "manifest.json", metadata, 0o644); err != nil {
		f.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		f.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(output+".tmp", output)
}

func writeTarFile(tw *tar.Writer, name string, data []byte, mode int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// Install 校验平台列表、文件哈希与面板版本，再原子切换当前 Agent 包。
func Install(archive, dataDir, panelVersion string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	base := filepath.Join(dataDir, "agent-bundles")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(base, ".stage-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	allowed := map[string]bool{"manifest.json": true}
	for _, p := range Platforms {
		allowed[FileName(p)] = true
	}
	seen := make(map[string]bool)
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if !allowed[h.Name] || seen[h.Name] || h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > maxBundleFileSize {
			return "", fmt.Errorf("Agent 包包含非法或重复文件：%s", h.Name)
		}
		seen[h.Name] = true
		out, err := os.OpenFile(filepath.Join(stage, h.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		_, copyErr := io.CopyN(out, tr, h.Size)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			return "", fmt.Errorf("解包 %s 失败：%v %v", h.Name, copyErr, closeErr)
		}
	}
	if len(seen) != len(allowed) {
		return "", fmt.Errorf("Agent 包缺少平台文件或 manifest.json")
	}
	metadata, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return "", err
	}
	var manifest Manifest
	if err := json.Unmarshal(metadata, &manifest); err != nil {
		return "", err
	}
	if manifest.PanelVersion != panelVersion || !versionPattern.MatchString(manifest.AgentVersion) || len(manifest.Files) != len(Platforms) {
		return "", fmt.Errorf("Agent 包版本或平台列表与面板不匹配")
	}
	for _, p := range Platforms {
		name := FileName(p)
		sum, err := fileSHA256(filepath.Join(stage, name))
		if err != nil || !strings.EqualFold(sum, manifest.Files[name]) {
			return "", fmt.Errorf("Agent 包文件校验失败：%s", name)
		}
	}
	// 同一个 Agent 版本可以随不同面板版本重新打包；按面板发布版本隔离目录。
	target := filepath.Join(base, manifest.PanelVersion)
	if _, err := os.Stat(target); err == nil {
		old, readErr := os.ReadFile(filepath.Join(target, "manifest.json"))
		if readErr != nil || !equalManifest(old, metadata) {
			return "", fmt.Errorf("面板版本 %s 的 Agent 包已存在但内容不同，请提升 VERSION", manifest.PanelVersion)
		}
		for _, p := range Platforms {
			name := FileName(p)
			sum, hashErr := fileSHA256(filepath.Join(target, name))
			if hashErr != nil || !strings.EqualFold(sum, manifest.Files[name]) {
				return "", fmt.Errorf("已有 Agent 版本 %s 的文件损坏：%s", manifest.AgentVersion, name)
			}
		}
	} else if os.IsNotExist(err) {
		if err := os.Rename(stage, target); err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	return manifest.AgentVersion, nil
}

// Activate 将已校验的面板发布包设为当前版本。
func Activate(dataDir, panelVersion string) error {
	if !versionPattern.MatchString(panelVersion) {
		return fmt.Errorf("面板版本号无效")
	}
	base := filepath.Join(dataDir, "agent-bundles")
	if _, err := os.Stat(filepath.Join(base, panelVersion, "manifest.json")); err != nil {
		return err
	}
	tmp := filepath.Join(base, "current.tmp")
	if err := os.WriteFile(tmp, []byte(panelVersion), 0o644); err != nil {
		return err
	}
	return replaceFile(tmp, filepath.Join(base, "current"))
}

func replaceFile(tmp, target string) error {
	if err := os.Rename(tmp, target); err == nil {
		return nil
	} else if _, statErr := os.Stat(target); statErr != nil {
		return err
	}
	// Windows 不允许 Rename 覆盖已有文件：先保留旧文件，失败时恢复。
	backup := target + ".bak"
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("旧备份 %s 尚未处理", backup)
	}
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func CurrentFile(dataDir, name string) (string, string, string, error) {
	base := filepath.Join(dataDir, "agent-bundles")
	value, err := os.ReadFile(filepath.Join(base, "current"))
	if err != nil {
		return "", "", "", err
	}
	panelVersion := strings.TrimSpace(string(value))
	if !versionPattern.MatchString(panelVersion) {
		return "", "", "", fmt.Errorf("当前面板发布版本号无效")
	}
	bundleDir := filepath.Join(base, panelVersion)
	metadata, err := os.ReadFile(filepath.Join(bundleDir, "manifest.json"))
	if err != nil {
		return "", "", "", err
	}
	var manifest Manifest
	if err := json.Unmarshal(metadata, &manifest); err != nil {
		return "", "", "", err
	}
	expected := manifest.Files[name]
	if manifest.PanelVersion != panelVersion || !versionPattern.MatchString(manifest.AgentVersion) || len(expected) != 64 {
		return "", "", "", fmt.Errorf("当前 Agent 包清单无效")
	}
	return filepath.Join(bundleDir, name), manifest.AgentVersion, expected, nil
}

func equalManifest(a, b []byte) bool {
	var x, y Manifest
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil &&
		x.PanelVersion == y.PanelVersion && x.AgentVersion == y.AgentVersion && reflect.DeepEqual(x.Files, y.Files)
}

func Checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSHA256(path string) (string, error) { return Checksum(path) }
