package frp

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalApplyVerifiesAndRestoresRunningConfiguration(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.MkdirAll(bin, 0700)
	m := NewManager(bin, dir)
	if out, err := exec.Command("go", "build", "-o", m.BinPath(), "./testdata/fakefrps").CombinedOutput(); err != nil {
		t.Fatalf("构建测试进程：%v %s", err, out)
	}
	if runtime.GOOS != "windows" {
		// 模拟 Docker 数据卷中旧文件缺少执行位；校验先于 start，须自行修复。
		if err := os.Chmod(m.BinPath(), 0644); err != nil {
			t.Fatal(err)
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	content := fmt.Sprintf(`{"bindAddr":"127.0.0.1","bindPort":%d}`, port)
	result := m.Apply(1, content, "start")
	if !result.OK || !result.Running {
		t.Fatalf("启动：%+v", result)
	}
	t.Cleanup(func() { m.Stop(1) })
	rejected := m.Apply(1, `{"failVerify":true}`, "apply")
	if rejected.OK || !m.Running(1) {
		t.Fatalf("校验失败影响原进程：%+v", rejected)
	}
	data, _ := os.ReadFile(m.ConfigPath(1))
	if string(data) != content {
		t.Fatal("校验失败污染了原配置")
	}
	failed := m.Apply(1, fmt.Sprintf(`{"bindPort":%d,"failStart":true}`, port), "apply")
	if failed.OK || !failed.RolledBack || !failed.Running {
		t.Fatalf("未恢复原服务：%+v", failed)
	}
	data, _ = os.ReadFile(m.ConfigPath(1))
	if string(data) != content {
		t.Fatal("运行失败未恢复原配置")
	}
	// 停止不依赖配置文件存在或内容有效。
	os.Remove(m.ConfigPath(1))
	if err := m.Stop(1); err != nil || m.Running(1) {
		t.Fatalf("缺失配置导致停止失败：%v", err)
	}
	result = m.Apply(1, content, "apply")
	if !result.OK || result.Running || m.Running(1) {
		t.Fatalf("停止状态的应用意外拉起进程：%+v", result)
	}
}

func TestLocalApplyUnavailableBinaryPreservesConfiguration(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "bin"), dir)
	const old = `{"bindPort":7000}`
	if err := os.WriteFile(m.ConfigPath(1), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	result := m.Apply(1, `{"bindPort":7001}`, "start")
	if result.OK || result.Running || !strings.Contains(result.Message, "frps 二进制不可用") {
		t.Fatalf("不可用二进制应在写入配置前失败：%+v", result)
	}
	data, err := os.ReadFile(m.ConfigPath(1))
	if err != nil || string(data) != old {
		t.Fatalf("原配置被改变：%s，错误=%v", data, err)
	}
	if _, err := os.Stat(m.ConfigPath(1) + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("不应保存候选配置或备份：%v", err)
	}
}
