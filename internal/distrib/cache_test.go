package distrib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListCachedBinaries(t *testing.T) {
	dir := t.TempDir()
	// frps（active 槽位）与未知平台的 riscv64 都不该被算进缓存清单
	for _, name := range []string{
		"frps-0.62.1-linux-amd64",
		"frpc-0.62.1-linux-amd64",
		"frps-0.61.1-windows-amd64.exe",
		"frps",
		"frpc-0.62.1-linux-riscv64",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	list := ListCachedBinaries(dir)
	if len(list) != 3 {
		t.Fatalf("期望 3 条缓存，实际 %d：%+v", len(list), list)
	}
	// 同类型按版本倒序，类型之间按 frpc → frps
	if list[0].Kind != "frpc" || list[0].Version != "0.62.1" {
		t.Fatalf("第一条应当是 frpc 0.62.1，实际 %+v", list[0])
	}
	if list[1].Kind != "frps" || list[1].Version != "0.62.1" || list[1].OS != "linux" {
		t.Fatalf("第二条应当是 frps 0.62.1 linux，实际 %+v", list[1])
	}
	if list[2].Version != "0.61.1" || list[2].OS != "windows" {
		t.Fatalf("第三条应当是 0.61.1 windows，实际 %+v", list[2])
	}

	if err := RemoveCachedBinary(dir, "frps", "0.62.1", "linux", "amd64"); err != nil {
		t.Fatalf("删除已缓存的二进制失败：%v", err)
	}
	if left := ListCachedBinaries(dir); len(left) != 2 {
		t.Fatalf("删除后应剩 2 条，实际 %d", len(left))
	}
	// 版本号不合法直接拦下，不会碰 active 槽位
	if err := RemoveCachedBinary(dir, "frps", "", "linux", "amd64"); err == nil {
		t.Fatal("空版本号应当报错")
	}
	if _, err := os.Stat(filepath.Join(dir, "frps")); err != nil {
		t.Fatalf("active 槽位不该被删掉：%v", err)
	}
	if err := RemoveCachedBinary(dir, "frps", "0.60.0", "linux", "amd64"); err == nil {
		t.Fatal("删除不存在的缓存应当报错")
	}
}

// 删除端点的 os/arch 来自查询参数：拼出的路径必须仍落在 bin 目录内
func TestRemoveCachedBinaryRejectsPathOutsideDir(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "frps-should-not-be-deleted")
	if err := os.WriteFile(outside, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := RemoveCachedBinary(dir, "frps", "0.62.1", "..", ".."); err == nil {
		t.Fatal("拼出的路径越出 bin 目录时应当报错")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("目录外的文件不该被删掉：%v", err)
	}
}

func TestParseBinaryPlatform(t *testing.T) {
	goos, goarch := ParseBinaryPlatform("frps-0.62.1-windows-amd64.exe", "frps")
	if goos != "windows" || goarch != "amd64" {
		t.Fatalf("windows 平台解析错误：%s/%s", goos, goarch)
	}
	if goos, goarch = ParseBinaryPlatform("frpc-0.62.1-darwin-arm64", "frpc"); goos != "darwin" || goarch != "arm64" {
		t.Fatalf("darwin 平台解析错误：%s/%s", goos, goarch)
	}
	if goos, _ = ParseBinaryPlatform("frpc-0.62.1-linux-riscv64", "frpc"); goos != "" {
		t.Fatalf("未知平台应当解析为空，实际 %s", goos)
	}
}
