package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/config"
	"dfpanel/internal/distrib"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
)

// newTestFrpHandler 用临时目录搭一个只做版本缓存判断的 handler
func newTestFrpHandler(t *testing.T) *FrpHandler {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DataDir: dir}
	return NewFrpHandler(cfg, frp.NewManager(filepath.Join(dir, "bin"), dir), agenthub.New())
}

func TestEnsurePanelCache(t *testing.T) {
	h := newTestFrpHandler(t)
	if err := os.MkdirAll(h.binDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	// 面板一份都没下载：拦下来，并把缺哪一份、去哪下载说清楚
	err := h.ensurePanelCache(&model.Agent{OS: "linux", Arch: "amd64", Roles: "frps,frpc"}, "0.62.1")
	if err == nil {
		t.Fatal("面板没有缓存时应当报错")
	}
	for _, want := range []string{"frps", "frpc", "0.62.1", "linux/amd64", "设置"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误提示要包含 %q，实际：%v", want, err)
		}
	}

	// Agent 自己手里已有该版本：切换只是换槽位，不该拦
	if err := h.ensurePanelCache(
		&model.Agent{OS: "linux", Arch: "amd64", Roles: "frpc", FRPCachedVersions: "0.62.1,0.61.1"}, "0.62.1",
	); err != nil {
		t.Fatalf("Agent 已缓存该版本时不应拦截：%v", err)
	}

	// 只给 frpc 角色补齐 frpc：frps 不参与，不该被要求
	path := filepath.Join(h.binDir(), distrib.BinaryName("frpc", "0.62.1", "linux", "amd64"))
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.ensurePanelCache(&model.Agent{OS: "linux", Arch: "amd64", Roles: "frpc"}, "0.62.1"); err != nil {
		t.Fatalf("面板已有对应二进制时不应拦截：%v", err)
	}
	// 换成 frps 角色就该被拦（这份没下载）
	if err := h.ensurePanelCache(&model.Agent{OS: "linux", Arch: "amd64", Roles: "frps"}, "0.62.1"); err == nil {
		t.Fatal("缺 frps 时应当报错")
	}

	// 平台还没上报：判断不了要哪一份，交给 Agent 自己处理
	if err := h.ensurePanelCache(&model.Agent{Roles: "frpc"}, "0.62.1"); err != nil {
		t.Fatalf("平台未知时不应拦截：%v", err)
	}
}
