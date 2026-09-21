package distrib

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidVersionRejectsTraversal(t *testing.T) {
	// version 来自免鉴权的 URL 参数，会被拼进可配置的下载模板，必须严格白名单
	bad := []string{
		"../../etc/passwd",
		"0.62",
		"latest",
		"",
		"v0.62.1",
		"0.62.1/../../x",
		"0.62.1;rm -rf /",
		"http://evil.example.com/x",
	}
	for _, v := range bad {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = true，应当拒绝", v)
		}
	}

	good := []string{"0.62.1", "0.61.0", "0.62.1-beta.1", "1.0.0"}
	for _, v := range good {
		if !ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = false，应当接受", v)
		}
	}
}

func TestExpandBase(t *testing.T) {
	official := DefaultDownloadBase
	got, err := ExpandBase(official, "0.62.1", "linux", "amd64")
	if err != nil {
		t.Fatalf("官方模板展开失败：%v", err)
	}
	want := "https://github.com/fatedier/frp/releases/download/v0.62.1/frp_0.62.1_linux_amd64.tar.gz"
	if got != want {
		t.Errorf("展开结果 = %q，期望 %q", got, want)
	}

	// 代理前缀形态：模板里出现两次 github 地址也应正确替换
	proxy := "https://ghproxy.net/https://github.com/fatedier/frp/releases/download/v{version}/{asset}"
	got, err = ExpandBase(proxy, "0.62.1", "windows", "amd64")
	if err != nil {
		t.Fatalf("代理模板展开失败：%v", err)
	}
	if got != "https://ghproxy.net/https://github.com/fatedier/frp/releases/download/v0.62.1/frp_0.62.1_windows_amd64.zip" {
		t.Errorf("代理模板展开结果不符：%q", got)
	}

	// 缺少占位符必须报错
	if _, err := ExpandBase("https://example.com/frp.tar.gz", "0.62.1", "linux", "amd64"); err == nil {
		t.Error("缺少 {version}/{asset} 占位符时应报错")
	}
	// 非法版本号必须报错，且不能出现在最终 URL 中
	if _, err := ExpandBase(official, "../../x", "linux", "amd64"); err == nil {
		t.Error("非法版本号应被拒绝")
	}
	// 空模板回退到默认模板
	if got, err := ExpandBase("", "0.62.1", "linux", "amd64"); err != nil || got != want {
		t.Errorf("空模板应回退为默认模板，得到 %q err=%v", got, err)
	}
}

func TestBinaryNameRoundTrip(t *testing.T) {
	name := BinaryName("frps", "0.62.1", "linux", "amd64")
	if name != "frps-0.62.1-linux-amd64" {
		t.Errorf("BinaryName = %q", name)
	}
	if v := ParseBinaryName(name, "frps"); v != "0.62.1" {
		t.Errorf("ParseBinaryName 反解 = %q，期望 0.62.1", v)
	}

	win := BinaryName("frpc", "0.62.1", "windows", "amd64")
	if win != "frpc-0.62.1-windows-amd64.exe" {
		t.Errorf("windows BinaryName = %q", win)
	}
	if v := ParseBinaryName(win, "frpc"); v != "0.62.1" {
		t.Errorf("windows 反解 = %q", v)
	}

	// 带横杠的预发布版本也要能反解
	pre := BinaryName("frps", "0.62.1-beta.1", "linux", "arm64")
	if v := ParseBinaryName(pre, "frps"); v != "0.62.1-beta.1" {
		t.Errorf("预发布版本反解 = %q，期望 0.62.1-beta.1", v)
	}

	// active 槽位名不含版本
	if ActiveName("frps", "linux") != "frps" || ActiveName("frps", "windows") != "frps.exe" {
		t.Error("ActiveName 结果不符")
	}
}

func TestMergeAndSortVersions(t *testing.T) {
	got := MergeVersions([]string{"0.62.1", "0.9.0", "0.10.0"}, []string{"0.61.1", "0.62.1"})
	want := []string{"0.62.1", "0.61.1", "0.10.0", "0.9.0"}
	if len(got) != len(want) {
		t.Fatalf("合并去重后长度 = %d，期望 %d（%v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序结果 = %v，期望 %v", got, want)
		}
	}
}

func TestCachedVersionsAndActivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		// 激活逻辑在 Windows 走拷贝分支，这里只验证扫描部分
		defer func() {}()
	}
	dir := t.TempDir()
	goos, goarch := runtime.GOOS, runtime.GOARCH

	// 造两个版本的文件 + 一个不该被识别的干扰文件
	for _, ver := range []string{"0.62.1", "0.61.1"} {
		if err := os.WriteFile(filepath.Join(dir, BinaryName("frps", ver, goos, goarch)), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "frps-1.json"), []byte("{}"), 0o644)

	got := CachedVersions(dir, "frps", goos, goarch)
	if len(got) != 2 || got[0] != "0.62.1" || got[1] != "0.61.1" {
		t.Fatalf("CachedVersions = %v，期望 [0.62.1 0.61.1]", got)
	}

	active := filepath.Join(dir, ActiveName("frps", goos))
	old, err := ActivateBinary(dir, active, "frps", "0.61.1", goos, goarch)
	if err != nil {
		t.Fatalf("ActivateBinary 失败：%v", err)
	}
	if old != "" {
		t.Errorf("首次激活前无 active 版本，应返回空串，得到 %q", old)
	}
	fi, err := os.Stat(active)
	if err != nil || fi.Size() == 0 {
		t.Fatalf("active 槽位未生成：%v", err)
	}
	if v := ActiveVersion(active, versionSidecar(dir, "frps")); v != "0.61.1" {
		t.Errorf("ActiveVersion = %q，期望 0.61.1", v)
	}

	// 再切到另一个版本，应返回旧版本号（回滚依据）
	old, err = ActivateBinary(dir, active, "frps", "0.62.1", goos, goarch)
	if err != nil {
		t.Fatalf("二次激活失败：%v", err)
	}
	if old != "0.61.1" {
		t.Errorf("二次激活返回旧版本 = %q，期望 0.61.1", old)
	}

	// 未下载的版本不能激活
	if _, err := ActivateBinary(dir, active, "frps", "0.99.9", goos, goarch); err == nil {
		t.Error("激活未下载的版本应报错")
	}
	// 非法版本号不能激活
	if _, err := ActivateBinary(dir, active, "frps", "../x", goos, goarch); err == nil {
		t.Error("激活非法版本号应报错")
	}
}
