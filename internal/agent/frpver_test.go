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
