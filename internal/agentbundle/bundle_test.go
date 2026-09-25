package agentbundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testArchive(t *testing.T, panelVersion string, corrupt bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	m := Manifest{PanelVersion: panelVersion, AgentVersion: "0.0.1-beta.18", Files: map[string]string{}}
	for _, p := range Platforms {
		name := FileName(p)
		data := []byte("agent-" + name)
		sum := sha256.Sum256(data)
		m.Files[name] = hex.EncodeToString(sum[:])
		if corrupt && name == "agent-linux-amd64" {
			data[0] = 'X'
		}
		if err := writeTarFile(tw, name, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	metadata, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(tw, "manifest.json", metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstallAndActivateAllPlatforms(t *testing.T) {
	dataDir := t.TempDir()
	archive := testArchive(t, "0.0.1-beta.19", false)
	version, err := Install(archive, dataDir, "0.0.1-beta.19")
	if err != nil || version != "0.0.1-beta.18" {
		t.Fatalf("安装失败：%s %v", version, err)
	}
	if _, _, _, err := CurrentFile(dataDir, "agent-linux-amd64"); !os.IsNotExist(err) {
		t.Fatalf("未激活前不应分发新文件：%v", err)
	}
	if err := Activate(dataDir, "0.0.1-beta.19"); err != nil {
		t.Fatal(err)
	}
	for _, p := range Platforms {
		path, gotVersion, _, err := CurrentFile(dataDir, FileName(p))
		if err != nil || gotVersion != version {
			t.Fatalf("%s/%s：%s %v", p.OS, p.Arch, gotVersion, err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Install(archive, dataDir, "0.0.1-beta.19"); err != nil {
		t.Fatalf("同版本重装应幂等：%v", err)
	}
}

func TestInstallRejectsWrongVersionAndChecksum(t *testing.T) {
	for _, tt := range []struct {
		name    string
		panel   string
		corrupt bool
	}{
		{"wrong-panel", "0.0.1-beta.17", false},
		{"bad-hash", "0.0.1-beta.19", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			if _, err := Install(testArchive(t, tt.panel, tt.corrupt), dataDir, "0.0.1-beta.19"); err == nil {
				t.Fatal("应拒绝不匹配的 Agent 包")
			}
			if _, _, _, err := CurrentFile(dataDir, "agent-linux-amd64"); !os.IsNotExist(err) {
				t.Fatal("失败后不应切换当前版本")
			}
		})
	}
}

func TestInstallRejectsArchiveTraversal(t *testing.T) {
	dataDir := t.TempDir()
	archive := filepath.Join(t.TempDir(), "bad.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := writeTarFile(tw, "../outside", []byte("bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(archive, dataDir, "0.0.1-beta.19"); err == nil {
		t.Fatal("应拒绝归档路径穿越")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "outside")); !os.IsNotExist(err) {
		t.Fatal("不得写出目标目录")
	}
}

func TestReplaceFileKeepsCompletedOutput(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "current")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "current.tmp")
	if err := os.WriteFile(tmp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(tmp, target); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(target)
	if err != nil || string(value) != "new" {
		t.Fatalf("替换失败：%s %v", value, err)
	}
}
