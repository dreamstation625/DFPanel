package agent

import (
	"os"
	"testing"

	"dfpanel/internal/proto"
)

func TestUnassignClearsOnlyBoundTarget(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"}
	a := New(cfg)
	bound := Target{Type: proto.TargetNode, ID: 11}
	other := Target{Type: proto.TargetNode, ID: 12}
	for _, target := range []Target{bound, other} {
		if err := os.WriteFile(cfg.ConfigPath(target), []byte(`{"serverAddr":"example"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		a.saveHistory(target, 1, `{"serverAddr":"example"}`)
		if err := a.updateState(targetKey(target), func(s *instanceState) { s.AutoStart = true }); err != nil {
			t.Fatal(err)
		}
	}
	result := a.handleCommand(proto.CommandData{Type: proto.CmdUnassign, TargetType: proto.TargetNode, TargetID: bound.ID})
	if !result.OK {
		t.Fatalf("清理失败：%s", result.Message)
	}
	if _, err := os.Stat(cfg.ConfigPath(bound)); !os.IsNotExist(err) {
		t.Fatalf("旧配置未删除：%v", err)
	}
	if _, err := os.Stat(a.historyPath(bound, 1)); !os.IsNotExist(err) {
		t.Fatalf("旧历史未删除：%v", err)
	}
	if _, ok := a.trackedState(targetKey(bound)); ok {
		t.Fatal("旧状态未删除")
	}
	if _, err := os.Stat(cfg.ConfigPath(other)); err != nil {
		t.Fatalf("其它配置不应受影响：%v", err)
	}
	if _, ok := a.trackedState(targetKey(other)); !ok {
		t.Fatal("其它状态不应受影响")
	}
}
