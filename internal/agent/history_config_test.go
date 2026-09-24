package agent

import (
	"testing"

	"dfpanel/internal/proto"
)

// 面板记录缺失时，Agent 只返回指定节点、指定版本的本地快照。
func TestVersionConfigCommandReadsTargetSnapshot(t *testing.T) {
	a := New(&Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"})
	target := Target{Type: proto.TargetNode, ID: 1}
	content := `{"serverAddr":"old.example"}`
	a.saveHistory(target, 2, content)
	a.saveHistory(Target{Type: proto.TargetNode, ID: 3}, 2, `{"serverAddr":"other.example"}`)

	result := a.handleCommand(proto.CommandData{
		Type: proto.CmdVersionConfig, TargetType: proto.TargetNode, TargetID: 1, Version: 2,
	})
	if !result.OK || result.Content != content {
		t.Fatalf("未返回指定历史配置：%+v", result)
	}
	result = a.handleCommand(proto.CommandData{
		Type: proto.CmdVersionConfig, TargetType: proto.TargetNode, TargetID: 1, Version: 3,
	})
	if result.OK || result.Content != "" {
		t.Fatalf("不存在的版本不应返回其他配置：%+v", result)
	}
}
