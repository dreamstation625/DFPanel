package distrib

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDownloadBase 默认下载地址模板。
	// {version} 为不带 v 前缀的版本号，{asset} 为压缩包文件名，另支持 {os} {arch}
	DefaultDownloadBase = "https://github.com/fatedier/frp/releases/download/v{version}/{asset}"
	// VersionListAPI 版本列表固定使用 GitHub 官方接口（不做镜像，避免第三方代理失效带来歧义）
	VersionListAPI = "https://api.github.com/repos/fatedier/frp/releases"
	// LatestTag 代指最新版本，仅在面板侧解析，下发给 Agent 的永远是具体版本号
	LatestTag = "latest"
	// versionListCacheTTL 版本列表内存缓存时长，规避 GitHub API 未认证 60 次/小时的限流
	versionListCacheTTL = 10 * time.Minute
)

// versionRe 版本号白名单。
// version 来自免鉴权的 URL 参数，且会被拼进可配置的下载模板，必须严格校验以防路径穿越。
var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$`)

// versionOutRe 从 frp -v 输出中提取版本号
var versionOutRe = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// ValidVersion 校验版本号格式
func ValidVersion(v string) bool { return versionRe.MatchString(v) }

// AssetName frp release 压缩包名：frp_<ver>_<os>_<arch>.tar.gz / .zip
func AssetName(version, goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("frp_%s_%s_%s.zip", version, normalizeOS(goos), normalizeArch(goarch))
	}
	return fmt.Sprintf("frp_%s_%s_%s.tar.gz", version, normalizeOS(goos), normalizeArch(goarch))
}

// BinaryName 版本化二进制文件名：frps-0.62.1-linux-amd64 / frpc-0.62.1-windows-amd64.exe
func BinaryName(kind, version, goos, goarch string) string {
	name := fmt.Sprintf("%s-%s-%s-%s", kind, version, normalizeOS(goos), normalizeArch(goarch))
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ParseBinaryName 从版本化文件名反解版本号，如 frps-0.62.1-linux-amd64 -> 0.62.1
//
// 不能按 "-" 简单切分：版本号自身可能带横杠（0.62.1-beta.1），
// 因此改为剥掉已知的 -<os>-<arch> 后缀。
func ParseBinaryName(name, kind string) string {
	base := strings.TrimSuffix(strings.TrimPrefix(name, kind+"-"), ".exe")
	for _, goos := range []string{"linux", "windows", "darwin"} {
		for _, goarch := range []string{"amd64", "arm64", "arm"} {
			suffix := "-" + goos + "-" + goarch
			if !strings.HasSuffix(base, suffix) {
				continue
			}
			if v := strings.TrimSuffix(base, suffix); ValidVersion(v) {
				return v
			}
		}
	}
	return ""
}

// ActiveName active 槽位名：frps / frps.exe（路径字面固定，现有 verify / start 链路无需改动）
func ActiveName(kind, goos string) string {
	if goos == "windows" {
		return kind + ".exe"
	}
	return kind
}

// ExpandBase 展开下载地址模板
func ExpandBase(base, version, goos, goarch string) (string, error) {
	if !ValidVersion(version) {
		return "", fmt.Errorf("版本号格式不合法：%s", version)
	}
	if strings.TrimSpace(base) == "" {
		base = DefaultDownloadBase
	}
	if !strings.Contains(base, "{version}") || !strings.Contains(base, "{asset}") {
		return "", errors.New("下载地址模板必须同时包含 {version} 与 {asset} 占位符")
	}
	asset := AssetName(version, goos, goarch)
	repl := strings.NewReplacer(
		"{version}", version,
		"{asset}", asset,
		"{os}", normalizeOS(goos),
		"{arch}", normalizeArch(goarch),
	)
	return repl.Replace(base), nil
}

// resolveVersion 把 latest 解析为具体版本号
func resolveVersion(version string) (string, error) {
	if version == "" || version == LatestTag {
		return LatestVersion()
	}
	if !ValidVersion(version) {
		return "", fmt.Errorf("版本号格式不合法：%s", version)
	}
	return version, nil
}

// EnsureFRPBinary 确保本地存在指定版本的 frp 二进制，缺失时按 base 模板下载并落为版本化文件。
//
// 缓存键包含版本号，因此多个版本可以共存、互不覆盖（这同时也是更新时的天然回滚备份）。
// 返回版本化文件的绝对路径。
func EnsureFRPBinary(kind, version, goos, goarch, dir, base string) (string, error) {
	if kind != "frps" && kind != "frpc" {
		return "", fmt.Errorf("未知类型：%s", kind)
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	ver, err := resolveVersion(version)
	if err != nil {
		return "", err
	}

	dest := filepath.Join(dir, BinaryName(kind, ver, goos, goarch))
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		// 命中缓存也要确认可执行：手动拷进来的那份往往没有执行位，
		// 否则启动时报一句 fork/exec ...: permission denied，很难往这上面想
		_ = EnsureExecutable(dest)
		return dest, nil
	}

	url, err := ExpandBase(base, ver, goos, goarch)
	if err != nil {
		return "", err
	}
	asset := AssetName(ver, goos, goarch)

	tmp, err := os.CreateTemp("", "frp-download-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// 下载专用 client：不给「整个请求」设短超时 —— 包有十几 MB，镜像站慢的时候
	// 5 分钟总超时会在一半处直接掐断（context deadline exceeded while reading body）；
	// 只限制等响应头的时间，连不上就快速失败。
	client := &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
	}

	// 慢下载、镜像站限速都常见，失败就重来几次（每次都是新请求，不做断点续传）
	const attempts = 3
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 2 * time.Second)
		}
		if lastErr = downloadToFile(client, url, tmp); lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return "", fmt.Errorf("下载 %s 失败（已重试 %d 次）：%w", asset, attempts, lastErr)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	switch {
	case strings.HasSuffix(asset, ".zip"):
		if err := extractZip(tmp.Name(), kind, dest); err != nil {
			return "", err
		}
	default:
		if err := extractTarGz(tmp, kind, dest); err != nil {
			return "", err
		}
	}
	_ = os.Chmod(dest, 0o755)

	// 下载后先自检，避免把一个跑不起来的二进制落进仓库
	if got, err := QueryVersion(dest); err == nil && got != ver {
		_ = os.Remove(dest)
		return "", fmt.Errorf("下载的二进制版本与期望不一致：期望 %s，实际 %s", ver, got)
	}
	return dest, nil
}

// EnsureExecutable 确保二进制可执行（chmod 0755，跟随软链）。
//
// 手动拷进来的文件、以及落在 Windows/NAS 共享目录里的文件常常没有执行位；
// 启动前补一刀，比让人对着 permission denied 猜要强。chmod 失败不返回错误 ——
// 只读挂载等场景下补不了，真正的结果交给 exec 报。
func EnsureExecutable(path string) error {
	fi, err := os.Stat(path) // 跟随软链
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%s 是目录，不是 frp 二进制", path)
	}
	if fi.Mode().Perm()&0o111 == 0o111 {
		return nil
	}
	_ = os.Chmod(path, 0o755)
	return nil
}

// ExecHint 给 exec 的权限类报错补一句人话：只补最常见的两种成因，其余保持原样
func ExecHint(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if !strings.Contains(msg, "permission denied") {
		return ""
	}
	return "（二进制没有执行权限，或所在目录是不可执行的挂载（noexec）→ 检查数据目录的挂载方式，" +
		"不要放在 Windows / NAS 共享目录上；面板与 Agent 的数据目录也不要挂成 :ro）"
}

// ---------- 面板侧缓存清单 ----------

// CachedBinary 面板 bin 目录里已缓存的一份 frp 二进制（版本化文件，不含 active 槽位）
type CachedBinary struct {
	Kind    string    `json:"kind"`
	Version string    `json:"version"`
	OS      string    `json:"os"`
	Arch    string    `json:"arch"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// knownPlatforms 版本化文件名可能带的平台后缀，用于从文件名反解
var knownPlatforms = []struct{ goos, goarch string }{
	{"linux", "amd64"}, {"linux", "arm64"}, {"linux", "arm"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"}, {"windows", "386"},
}

// ParseBinaryPlatform 从版本化文件名反解平台，如 frps-0.62.1-linux-amd64 -> linux / amd64
func ParseBinaryPlatform(name, kind string) (string, string) {
	base := strings.TrimSuffix(strings.TrimPrefix(name, kind+"-"), ".exe")
	for _, p := range knownPlatforms {
		if strings.HasSuffix(base, "-"+p.goos+"-"+p.goarch) {
			return p.goos, p.goarch
		}
	}
	return "", ""
}

// ListCachedBinaries 扫描 dir 下全部版本化 frp 二进制，按类型、版本倒序排列。
// active 槽位是 frps / frps.exe 这种固定名，不会被算进来。
func ListCachedBinaries(dir string) []CachedBinary {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []CachedBinary{}
	}
	out := []CachedBinary{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		kind := ""
		switch {
		case strings.HasPrefix(name, "frps-"):
			kind = "frps"
		case strings.HasPrefix(name, "frpc-"):
			kind = "frpc"
		default:
			continue
		}
		version := ParseBinaryName(name, kind)
		if version == "" {
			continue
		}
		goos, goarch := ParseBinaryPlatform(name, kind)
		if goos == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, CachedBinary{
			Kind:    kind,
			Version: version,
			OS:      goos,
			Arch:    goarch,
			Name:    name,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Version != out[j].Version {
			return versionGreater(out[i].Version, out[j].Version)
		}
		if out[i].OS != out[j].OS {
			return out[i].OS < out[j].OS
		}
		return out[i].Arch < out[j].Arch
	})
	return out
}

// versionGreater 按数字段比较版本号，0.10.0 要排在 0.9.0 前面
func versionGreater(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa); i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var out [3]int
	fields := strings.Split(v, ".")
	for i := 0; i < len(out) && i < len(fields); i++ {
		n, _ := strconv.Atoi(fields[i])
		out[i] = n
	}
	return out
}

// RemoveCachedBinary 删除 dir 下指定的版本化二进制。
// 只认 <kind>-<ver>-<os>-<arch> 这种名字，active 槽位（frps / frps.exe）删不掉。
func RemoveCachedBinary(dir, kind, version, goos, goarch string) error {
	if kind != "frps" && kind != "frpc" {
		return fmt.Errorf("未知类型：%s", kind)
	}
	if !ValidVersion(version) {
		return fmt.Errorf("版本号格式不合法：%s", version)
	}
	name := BinaryName(kind, version, goos, goarch)
	path := filepath.Join(dir, name)
	if filepath.Dir(path) != filepath.Clean(dir) {
		return fmt.Errorf("路径不合法：%s", name)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("没有缓存 %s", name)
	}
	return os.Remove(path)
}

// downloadToFile 把 url 下载到 f（每次从头写）；网络中断或超时都返回错误，交给调用方重试
func downloadToFile(client *http.Client, url string, f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

// ActivateBinary 把某个版本切为 active 槽位，返回切换前的 active 版本（空表示原先无 active 二进制）。
//
// active 路径由调用方指定且字面不变（如 <dataDir>/frps），因此调用方无需重建任何持有该路径的缓存。
// 非 Windows 用软链原子替换；Windows 软链需要开发者模式，退化为「写 .new 再 rename 覆盖」。
// 注意：Windows 上正在运行的 exe 被占用无法替换，调用前必须先停掉相关实例。
func ActivateBinary(binDir, activePath, kind, version, goos, goarch string) (string, error) {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if !ValidVersion(version) {
		return "", fmt.Errorf("版本号格式不合法：%s", version)
	}

	src := filepath.Join(binDir, BinaryName(kind, version, goos, goarch))
	if fi, err := os.Stat(src); err != nil || fi.Size() == 0 {
		return "", fmt.Errorf("版本 %s 的 %s 二进制尚未下载，请先执行下载", version, kind)
	}
	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		return "", err
	}

	old := ActiveVersion(activePath, versionSidecar(binDir, kind))

	if goos != "windows" {
		tmpLink := activePath + ".new"
		_ = os.Remove(tmpLink)
		if err := os.Symlink(src, tmpLink); err == nil {
			if err := os.Rename(tmpLink, activePath); err == nil {
				_ = writeVersionSidecar(binDir, kind, version)
				return old, nil
			}
			_ = os.Remove(tmpLink)
		}
	}

	// 拷贝方式：先完整写入 .new 再 rename，避免半截文件被进程读到
	tmpFile := activePath + ".new"
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(tmpFile, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmpFile, activePath); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	_ = os.Chmod(activePath, 0o755)
	_ = writeVersionSidecar(binDir, kind, version)
	return old, nil
}

// ActiveVersion 查询 active 槽位对应的版本：优先读版本落签，缺失时执行二进制自报版本
func ActiveVersion(activePath, sidecar string) string {
	if sidecar != "" {
		if v, err := os.ReadFile(sidecar); err == nil {
			if s := strings.TrimSpace(string(v)); ValidVersion(s) {
				return s
			}
		}
	}
	if activePath == "" {
		return ""
	}
	if _, err := os.Stat(activePath); err != nil {
		return ""
	}
	if v, err := QueryVersion(activePath); err == nil {
		if sidecar != "" {
			_ = os.MkdirAll(filepath.Dir(sidecar), 0o755)
			_ = os.WriteFile(sidecar, []byte(v+"\n"), 0o644)
		}
		return v
	}
	return ""
}

// CachedVersions 列出本地已缓存的某个 kind 的版本（降序）
func CachedVersions(dir, kind, goos, goarch string) []string {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	out := []string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	prefix := kind + "-"
	suffix := fmt.Sprintf("-%s-%s", normalizeOS(goos), normalizeArch(goarch))
	if goos == "windows" {
		suffix += ".exe"
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		ver := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		if ValidVersion(ver) {
			out = append(out, ver)
		}
	}
	sortVersionsDesc(out)
	return out
}

// QueryVersion 执行 <bin> -v 解析版本号
func QueryVersion(binPath string) (string, error) {
	if _, err := os.Stat(binPath); err != nil {
		return "", err
	}
	for _, flag := range []string{"-v", "--version"} {
		out, err := runWithTimeout(5*time.Second, binPath, flag)
		if err != nil && len(out) == 0 {
			continue
		}
		if m := versionOutRe.FindString(string(out)); m != "" {
			return m, nil
		}
	}
	return "", errors.New("无法从二进制输出中解析版本号")
}

// 版本列表缓存
var (
	versionListMu  sync.Mutex
	versionListVal []string
	versionListAt  time.Time
)

// FallbackVersions 官方列表拿不到时的兜底版本（新到旧，都实测是可下载的 release）。
//
// frp 发版不快，这份列表旧一点没关系：用户随时可以手填更新的版本号。
var FallbackVersions = []string{
	"0.71.0", "0.70.0", "0.69.0", "0.68.0", "0.67.0", "0.66.0",
	"0.65.0", "0.64.0", "0.63.0", "0.62.1", "0.61.1", "0.60.0",
}

// ListVersions 取 frp 官方 release 版本列表（降序）。
// 命中 10 分钟内存缓存；失败时返回错误，由调用方降级为手填版本。
func ListVersions() ([]string, error) { return listVersions(false) }

// ListVersionsFresh 忽略缓存重新拉取，界面上点「重新获取」时用
func ListVersionsFresh() ([]string, error) { return listVersions(true) }

func listVersions(force bool) ([]string, error) {
	versionListMu.Lock()
	if !force && len(versionListVal) > 0 && time.Since(versionListAt) < versionListCacheTTL {
		cached := append([]string(nil), versionListVal...)
		versionListMu.Unlock()
		return cached, nil
	}
	versionListMu.Unlock()

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, VersionListAPI+"?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询版本列表失败：HTTP %d", resp.StatusCode)
	}

	var out []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(out))
	for _, r := range out {
		if r.Draft {
			continue
		}
		v := strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
		if ValidVersion(v) {
			versions = append(versions, v)
		}
	}
	sortVersionsDesc(versions)

	versionListMu.Lock()
	versionListVal = versions
	versionListAt = time.Now()
	versionListMu.Unlock()
	return versions, nil
}

// LatestVersion 查询 frp 最新版本号（不带 v 前缀）
func LatestVersion() (string, error) {
	if list, err := ListVersions(); err == nil && len(list) > 0 {
		return list[0], nil
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(VersionListAPI + "/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询最新版本失败：HTTP %d", resp.StatusCode)
	}
	var out struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(strings.TrimSpace(out.TagName), "v")
	if !ValidVersion(v) {
		return "", errors.New("无法解析最新版本号：" + out.TagName)
	}
	return v, nil
}

// MergeVersions 合并「可用版本」与「已缓存版本」，用于前端下拉展示
func MergeVersions(available, cached []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+len(cached))
	for _, v := range cached {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range available {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sortVersionsDesc(out)
	return out
}

func versionSidecar(dir, kind string) string {
	return filepath.Join(dir, kind+".version")
}

func writeVersionSidecar(dir, kind, version string) error {
	_ = os.MkdirAll(dir, 0o755)
	return os.WriteFile(versionSidecar(dir, kind), []byte(version+"\n"), 0o644)
}

// sortVersionsDesc 按数字段降序排序（非法版本排到最后）
func sortVersionsDesc(list []string) {
	sort.Slice(list, func(i, j int) bool { return compareVersion(list[i], list[j]) > 0 })
}

func compareVersion(a, b string) int {
	as, bs := strings.SplitN(a, ".", 3), strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, errors.New("执行超时")
	}
	return out, err
}

func extractTarGz(r io.Reader, kind, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	want := kind
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != want {
			continue
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	return errors.New("压缩包中未找到 " + want)
}

func extractZip(path, kind, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()

	want := kind + ".exe"
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("压缩包中未找到 " + want)
}

func normalizeOS(goos string) string {
	switch goos {
	case "darwin":
		return "darwin"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return "arm"
	}
}
