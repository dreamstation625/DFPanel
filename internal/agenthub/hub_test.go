package agenthub

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

func TestReplacingConnectionDoesNotBlockOrDisconnectNewConnection(t *testing.T) {
	h := New()
	old := &Conn{hub: h, agentID: 1, done: make(chan struct{})}
	h.conns[1] = old
	current := &Conn{hub: h, agentID: 1, done: make(chan struct{})}
	replaced := make(chan struct{})
	go func() {
		h.replace(current)
		close(replaced)
	}()
	select {
	case <-replaced:
	case <-time.After(2 * time.Second):
		t.Fatal("Agent 重连时替换连接被 Hub 锁阻塞")
	}
	if !h.Online(1) || h.conns[1] != current {
		t.Fatal("旧连接关闭后，新连接应保持在线")
	}
	select {
	case <-old.done:
	default:
		t.Fatal("旧连接未关闭")
	}
	old.close()
	if !h.Online(1) {
		t.Fatal("旧连接重复关闭不应清理新连接")
	}
}

// WebSocket 和 HTTP 心跳都会调用 UpdateState；版本必须由此落库供两个列表展示。
func TestUpdateStatePersistsFrpVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "agent.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Agent{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		_ = sqlDB.Close()
	})

	agent := model.Agent{Name: "测试 Agent", NodeKey: "test-key", Secret: "test-secret"}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	h := New()
	h.UpdateState(agent.ID, proto.HeartbeatData{
		Runtime:    "docker",
		FrpVersion: "0.71.0",
		FrpCached:  []string{"0.71.0", "0.70.0"},
	}, "127.0.0.1")

	var saved model.Agent
	if err := db.First(&saved, agent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Runtime != "docker" || saved.FRPInstalledVersion != "0.71.0" || saved.FRPCachedVersions != "0.71.0,0.70.0" {
		t.Fatalf("心跳版本未正确保存：runtime=%q active=%q cached=%q", saved.Runtime, saved.FRPInstalledVersion, saved.FRPCachedVersions)
	}

	// 短暂探测不到槽位时，保留上次确认的版本，避免页面误报未下发。
	h.UpdateState(agent.ID, proto.HeartbeatData{Runtime: "docker"}, "127.0.0.1")
	if err := db.First(&saved, agent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.FRPInstalledVersion != "0.71.0" {
		t.Fatalf("空版本心跳清掉了已知版本：%q", saved.FRPInstalledVersion)
	}
}

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
