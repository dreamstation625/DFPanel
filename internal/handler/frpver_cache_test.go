package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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
	err := h.ensurePanelCache(&model.Agent{OS: "linux", Arch: "amd64", Roles: "frpc"}, "0.62.1")
	if err == nil {
		t.Fatal("面板没有缓存时应当报错")
	}
	for _, want := range []string{"frpc", "0.62.1", "linux/amd64", "设置"} {
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

// 清单接口返回的是 { cached: [...] }，前端按这个结构取值 —— 直接返回数组会让列表渲染不出来
func TestCacheListAndDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newTestFrpHandler(t)
	if err := os.MkdirAll(h.binDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"frps-0.62.1-linux-amd64", "frpc-0.62.1-linux-amd64"} {
		if err := os.WriteFile(filepath.Join(h.binDir(), name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/frp/cache", nil)
	h.CacheList(c)
	var list struct {
		Cached []distrib.CachedBinary `json:"cached"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("清单响应不是 {cached:[...]}：%v，body=%s", err, rec.Body.String())
	}
	if len(list.Cached) != 2 {
		t.Fatalf("期望 2 条缓存，实际 %d", len(list.Cached))
	}

	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete,
		"/api/frp/cache?kind=frps&version=0.62.1&os=linux&arch=amd64", nil)
	h.CacheDelete(c)
	var del struct {
		Cached []distrib.CachedBinary `json:"cached"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &del); err != nil {
		t.Fatalf("删除响应不是 {cached:[...]}：%v", err)
	}
	if len(del.Cached) != 1 || del.Cached[0].Kind != "frpc" {
		t.Fatalf("删除后应只剩 frpc 一份：%+v", del.Cached)
	}
	if _, err := os.Stat(filepath.Join(h.binDir(), "frps-0.62.1-linux-amd64")); !os.IsNotExist(err) {
		t.Fatal("目标文件应当已被删除")
	}
}
