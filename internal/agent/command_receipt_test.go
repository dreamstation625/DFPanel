package agent

import (
	"dfpanel/internal/proto"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandReceiptSurvivesRestartAndDoesNotRepeatEffects(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), NodeKey: "key", Roles: "frpc", Runtime: "process"}
	a := New(cfg)
	cmd := proto.CommandData{CommandID: 1, Type: proto.CmdUnassign, TargetType: proto.TargetNode, TargetID: 1}
	if r := a.executeCommand(cmd); !r.OK {
		t.Fatal(r.Message)
	}
	// 再创建同路径文件可观测清理是否被再次执行。
	path := cfg.ConfigPath(Target{Type: proto.TargetNode, ID: 1})
	os.WriteFile(path, []byte("retained"), 0600)
	if r := New(cfg).executeCommand(cmd); !r.OK {
		t.Fatal(r.Message)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("重复命令再次删除了文件")
	}
	cmd.Payload = "changed"
	if r := a.executeCommand(cmd); r.OK {
		t.Fatal("同号不同内容被接受")
	}
}

func TestInterruptedCommandDoesNotExecuteBlindly(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), NodeKey: "key", Roles: "frpc", Runtime: "process"}
	a := New(cfg)
	cmd := proto.CommandData{CommandID: 2, Type: proto.CmdUnassign, TargetType: proto.TargetNode, TargetID: 1}
	b, _ := json.Marshal(cmd)
	receipt := filepath.Join(cfg.DataDir, "command-receipts", checksumOf(cfg.NodeKey), "2.json")
	if err := saveReceipt(receipt, commandReceipt{State: "running", Fingerprint: checksumOf(string(b))}); err != nil {
		t.Fatal(err)
	}
	path := cfg.ConfigPath(Target{Type: proto.TargetNode, ID: 1})
	os.WriteFile(path, []byte("retained"), 0600)
	if r := a.executeCommand(cmd); r.OK {
		t.Fatal("崩溃中的命令被冒充成功")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("未确认的命令被重复执行")
	}
}
