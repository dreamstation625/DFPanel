package distrib

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 执行位兜底：手动拷进来的二进制缺执行位时，启动前要能补上
func TestEnsureExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上没有 POSIX 执行位")
	}
	dir := t.TempDir()

	bin := filepath.Join(dir, "frps-0.62.1-linux-amd64")
	if err := os.WriteFile(bin, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExecutable(bin); err != nil {
		t.Fatalf("补执行位不该报错：%v", err)
	}
	fi, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("应当补上执行位，实际 %v", fi.Mode().Perm())
	}

	// 路径是目录（docker 挂载不存在的路径时会造出这种东西）要明确报出来
	sub := filepath.Join(dir, "frps")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureExecutable(sub); err == nil {
		t.Fatal("目录应当报错")
	}
}

// 命中缓存时同样要确认执行位：这份文件可能是人为放进 bin 目录的
func TestEnsureFRPBinaryFixesExecBitOnCacheHit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上没有 POSIX 执行位")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, BinaryName("frps", "0.62.1", runtime.GOOS, runtime.GOARCH))
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// base 指向不可达地址：命中缓存就不该去联网
	if _, err := EnsureFRPBinary("frps", "0.62.1", runtime.GOOS, runtime.GOARCH, dir, "http://127.0.0.1:1/{version}/{asset}"); err != nil {
		t.Fatalf("命中缓存不该报错：%v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("命中缓存时也应补执行位，实际 %v", fi.Mode().Perm())
	}
}

func TestExecHint(t *testing.T) {
	if got := ExecHint(errors.New("fork/exec /data/bin/frps: no such file or directory")); got != "" {
		t.Fatalf("非权限类错误不该加提示，实际：%s", got)
	}
	if got := ExecHint(errors.New("fork/exec /data/bin/frps: permission denied")); got == "" {
		t.Fatal("权限类错误应当补一句提示")
	}
	if got := ExecHint(nil); got != "" {
		t.Fatalf("nil 不该有提示，实际：%s", got)
	}
}
