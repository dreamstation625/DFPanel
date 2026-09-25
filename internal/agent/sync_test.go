package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"dfpanel/internal/proto"
)

func TestSyncMissingConfigsRestoresOwnRoleWithoutReplacingLocalFile(t *testing.T) {
	configs := []proto.ManagedConfig{
		{TargetType: proto.TargetNode, TargetID: 1, Version: 3, Content: `{"serverAddr":"panel.example"}`, AutoStart: true, ManualStopped: true},
		{TargetType: proto.TargetServer, TargetID: 2, Version: 4, Content: `{"bindPort":7000}`, AutoStart: true},
		{TargetType: proto.TargetNode, TargetID: 3, Version: 5, Content: `{"serverAddr":"new.example"}`, AutoStart: true},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/configs" || r.URL.Query().Get("sign") == "" {
			t.Errorf("unexpected request: %s", r.URL.String())
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"configs": configs})
	}))
	defer server.Close()

	cfg := &Config{DataDir: t.TempDir(), PanelURL: server.URL, NodeKey: "key", Secret: "secret", Roles: "frpc", Runtime: "process"}
	agent := New(cfg)
	local := Target{Type: proto.TargetNode, ID: 3}
	if err := os.WriteFile(cfg.ConfigPath(local), []byte(`{"serverAddr":"local.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.syncMissingConfigs(); err != nil {
		t.Fatal(err)
	}

	for _, expected := range []struct {
		target  Target
		content string
		version int
		auto    bool
		stopped bool
	}{
		{Target{Type: proto.TargetNode, ID: 1}, configs[0].Content, 3, true, true},
	} {
		data, err := os.ReadFile(cfg.ConfigPath(expected.target))
		if err != nil || string(data) != expected.content {
			t.Fatalf("配置恢复失败 %s: content=%q err=%v", targetKey(expected.target), data, err)
		}
		state, ok := agent.trackedState(targetKey(expected.target))
		if !ok || state.AutoStart != expected.auto || state.ManualStopped != expected.stopped {
			t.Fatalf("启动状态恢复失败 %s: %+v tracked=%v", targetKey(expected.target), state, ok)
		}
		if history, err := os.ReadFile(agent.historyPath(expected.target, expected.version)); err != nil || string(history) != expected.content {
			t.Fatalf("历史配置恢复失败 %s: content=%q err=%v", targetKey(expected.target), history, err)
		}
	}
	data, err := os.ReadFile(cfg.ConfigPath(local))
	if err != nil || string(data) != `{"serverAddr":"local.example"}` {
		t.Fatalf("已有配置被覆盖：content=%q err=%v", data, err)
	}
	if _, ok := agent.trackedState(targetKey(local)); ok {
		t.Fatal("已有配置的启动状态不应被面板快照改写")
	}
	if _, err := os.Stat(cfg.ConfigPath(Target{Type: proto.TargetServer, ID: 2})); !os.IsNotExist(err) {
		t.Fatalf("客户端 Agent 不应恢复服务端配置，结果：%v", err)
	}
}

func TestSyncMissingConfigsRetriesAfterPanelFailure(t *testing.T) {
	var available atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"configs": []proto.ManagedConfig{{
			TargetType: proto.TargetNode, TargetID: 1, Version: 1, Content: `{"serverAddr":"restored.example"}`,
		}}})
	}))
	defer server.Close()
	cfg := &Config{DataDir: t.TempDir(), PanelURL: server.URL, NodeKey: "key", Secret: "secret", Roles: "frpc"}
	agent := New(cfg)
	if err := agent.syncMissingConfigs(); err == nil {
		t.Fatal("面板失败时应返回错误，供启动循环重试")
	}
	available.Store(true)
	if err := agent.syncMissingConfigs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.ConfigPath(Target{Type: proto.TargetNode, ID: 1})); err != nil {
		t.Fatalf("面板恢复后未补齐配置：%v", err)
	}
}
