package agenthub

import (
	"testing"

	"dfpanel/internal/proto"
)

// 启停指令的结果要立刻反映到状态缓存里，不能等下一次心跳（30 秒）
func TestSetTargetRunning(t *testing.T) {
	h := New()
	if _, ok := h.State(1, proto.TargetServer, 2); ok {
		t.Fatal("初始不应有状态")
	}

	h.SetTargetRunning(1, proto.TargetServer, 2, true)
	st, ok := h.State(1, proto.TargetServer, 2)
	if !ok || !st.Running {
		t.Fatalf("期望运行中，实际 ok=%v running=%v", ok, st.Running)
	}

	h.SetTargetRunning(1, proto.TargetServer, 2, false)
	if st, _ := h.State(1, proto.TargetServer, 2); st.Running {
		t.Fatal("停止后状态仍是运行中")
	}

	// 同一 Agent 上的其它实例不受影响
	if _, ok := h.State(1, proto.TargetNode, 2); ok {
		t.Fatal("节点状态被服务端指令误改")
	}
	if _, ok := h.State(9, proto.TargetServer, 2); ok {
		t.Fatal("其它 Agent 的状态被误改")
	}
}
