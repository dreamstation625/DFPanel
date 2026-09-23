package agent

import (
	"os"
	"testing"
)

// 目标版本和当前一致时应当直接跳过：切换要停实例、换槽位再拉起，docker 运行时还要重建容器
func TestSameAsCurrent(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frps", Runtime: "process"}
	a := &Agent{cfg: cfg}

	if a.sameAsCurrent([]string{"frps"}, "0.71.0") {
		t.Error("没有任何版本落签时不该判定为「已经是目标版本」")
	}

	if err := os.MkdirAll(cfg.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.VersionSidecar("frps"), []byte("0.71.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !a.sameAsCurrent([]string{"frps"}, "0.71.0") {
		t.Error("落签已是 0.71.0，应判定为无需切换")
	}
	if a.sameAsCurrent([]string{"frps"}, "0.70.0") {
		t.Error("目标版本不同时不该判定为无需切换")
	}
	if a.sameAsCurrent([]string{"frps", "frpc"}, "0.71.0") {
		t.Error("frpc 还没有落签，不能因为 frps 一致就整体跳过")
	}
}

// 槽位里已经有二进制时不该再去面板下载（ensureRuntimeBinary 每次启动/应用都会调用）
func TestEnsureRuntimeBinarySkipsWhenSlotReady(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"}
	a := &Agent{cfg: cfg}

	slot := cfg.LocalBinaryPath("frpc")
	if err := os.WriteFile(slot, []byte("fake-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}

	created, err := a.ensureRuntimeBinary("frpc")
	if err != nil {
		t.Fatalf("槽位就绪时不该报错：%v", err)
	}
	if created {
		t.Error("槽位就绪时不该重建")
	}
}

// 没启用该角色时直接跳过，不做任何下载
func TestEnsureRuntimeBinarySkipsDisabledRole(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frps", Runtime: "process"}
	a := &Agent{cfg: cfg}

	created, err := a.ensureRuntimeBinary("frpc")
	if err != nil {
		t.Fatalf("未启用角色时不该报错：%v", err)
	}
	if created {
		t.Error("未启用的角色不该补槽位")
	}
}
