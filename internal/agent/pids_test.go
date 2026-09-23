package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPidFileReadWriteRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frps-1.pid")

	if got := readPidFile(path); got != 0 {
		t.Errorf("文件不存在时应返回 0，实际 %d", got)
	}
	if err := writePidFile(path, os.Getpid()); err != nil {
		t.Fatalf("写 pid 文件失败：%v", err)
	}
	if got := readPidFile(path); got != os.Getpid() {
		t.Errorf("读回 pid = %d，期望 %d", got, os.Getpid())
	}
	removePidFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("删除后文件不该还在")
	}
	// 内容不合法时按「没有」处理，不能让脏文件把接管逻辑带偏
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readPidFile(path); got != 0 {
		t.Errorf("内容不合法时应返回 0，实际 %d", got)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("当前进程应判定为存活")
	}
	if processAlive(0) {
		t.Error("pid 0 不该判定为存活")
	}
	if processAlive(999999) {
		t.Error("不存在的 pid 不该判定为存活")
	}
}

// Agent 重启后接管旧实例的两个关键分支：没有 pid 文件、pid 文件已过期
func TestAdoptSkipsStaleState(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{
		Kind:       "frps",
		Runtime:    "process",
		BinPath:    filepath.Join(dir, "frps.exe"),
		ConfigPath: filepath.Join(dir, "frps-1.json"),
		PidPath:    filepath.Join(dir, "frps-1.pid"),
	}
	ctrl := NewController(spec)

	if ctrl.Adopt() {
		t.Error("没有 pid 文件时不该接管")
	}
	if ctrl.Running() {
		t.Error("没接管也没启动时不该是运行状态")
	}

	// 写一个早已退出的 pid：应清掉 pid 文件并放弃接管
	if err := writePidFile(spec.PidPath, 999999); err != nil {
		t.Fatal(err)
	}
	if ctrl.Adopt() {
		t.Error("进程已退出时不该接管")
	}
	if _, err := os.Stat(spec.PidPath); !os.IsNotExist(err) {
		t.Error("过期的 pid 文件应被清掉")
	}

	// docker 运行时不走 pid 文件
	docker := NewController(Spec{Kind: "frps", Runtime: "docker", PidPath: spec.PidPath})
	if docker.Adopt() {
		t.Error("docker 运行时不该走 pid 接管")
	}
}
