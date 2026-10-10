package agenthub

import (
	"dfpanel/internal/proto"
	"testing"
	"time"
)

func TestResultMustMatchConnectionAndMessageType(t *testing.T) {
	h := New()
	owner := &Conn{agentID: 1}
	other := &Conn{agentID: 2}
	old := &Conn{agentID: 1}
	ch := make(chan proto.Envelope, 1)
	h.pending["cmd-1"] = pendingResult{conn: owner, ch: ch}
	for _, c := range []*Conn{other, old} {
		h.deliver(c, proto.Envelope{Type: proto.MsgCommandResult, ID: "cmd-1"})
	}
	h.deliver(owner, proto.Envelope{Type: proto.MsgPong, ID: "cmd-1"})
	select {
	case <-ch:
		t.Fatal("错误连接或消息类型满足了等待者")
	default:
	}
	h.deliver(owner, proto.Envelope{Type: proto.MsgCommandResult, ID: "cmd-1"})
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("归属正确的结果未送达")
	}
}
