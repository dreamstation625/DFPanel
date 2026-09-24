package distrib

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// docker 运行时的槽位要用拷贝：软链在宿主 daemon 那边解析不到
func TestCopyBinaryReplacesSymlink(t *testing.T) {
	dir := t.TempDir()
	goos, goarch := runtime.GOOS, runtime.GOARCH
	ver := "0.62.1"

	src := filepath.Join(dir, BinaryName("frpc", ver, goos, goarch))
	if err := os.WriteFile(src, []byte("frpc-body"), 0o755); err != nil {
		t.Fatal(err)
	}

	slot := filepath.Join(dir, "frpc-slot")
	sidecar := filepath.Join(dir, "frpc-container-"+goos+"-"+goarch+".version")
	if runtime.GOOS != "windows" {
		// 模拟 process 运行时留下的软链槽位
		if err := os.Symlink(src, slot); err != nil {
			t.Fatal(err)
		}
	}

	if err := CopyBinary(dir, slot, sidecar, "frpc", ver, goos, goarch); err != nil {
		t.Fatalf("拷贝到槽位失败：%v", err)
	}
	fi, err := os.Lstat(slot)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("槽位不该还是软链")
	}
	if body, err := os.ReadFile(slot); err != nil || string(body) != "frpc-body" {
		t.Fatalf("槽位内容不对：%q %v", string(body), err)
	}
	if v, err := os.ReadFile(sidecar); err != nil || string(v) != ver+"\n" {
		t.Fatalf("版本落签不对：%q %v", string(v), err)
	}

	// 版本还没下载时要说清楚，而不是留个空槽位
	if err := CopyBinary(dir, slot, sidecar, "frpc", "0.9.9", goos, goarch); err == nil {
		t.Fatal("缺版本时应当报错")
	}
	if err := CopyBinary(dir, slot, sidecar, "frpc", "../../etc/passwd", goos, goarch); err == nil {
		t.Fatal("非法版本号应当报错")
	}
}
