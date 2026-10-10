package handler

import (
	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteNodeCascadesChildrenButKeepsAuditHistory(t *testing.T) {
	securityDB(t)
	node := model.Node{Name: "test", NodeKey: "key"}
	database.DB.Create(&node)
	database.DB.Create(&model.Proxy{NodeID: node.ID, Name: "proxy"})
	database.DB.Create(&model.Visitor{NodeID: node.ID, Name: "visitor"})
	database.DB.Create(&model.ConfigVersion{TargetType: "node", TargetID: node.ID, Version: 1, Content: "snapshot"})
	r := ginRouter()
	r.POST("/api/nodes/:id/delete", NewNodeHandler(agenthub.New(), nil).Delete)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/nodes/1/delete", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, table := range []any{&model.Node{}, &model.Proxy{}, &model.Visitor{}} {
		var n int64
		database.DB.Model(table).Count(&n)
		if n != 0 {
			t.Fatalf("未清理 %T", table)
		}
	}
	var n int64
	database.DB.Model(&model.ConfigVersion{}).Count(&n)
	if n != 1 {
		t.Fatal("审计记录被误删除")
	}
}

func TestLocalStopDoesNotGenerateOrWriteConfiguration(t *testing.T) {
	securityDB(t)
	server := model.FrpsServer{Name: "test", DeployMode: "local", ExtraJSON: "{invalid"}
	database.DB.Create(&server)
	dir := filepath.Join(t.TempDir(), "missing")
	h := NewServerHandler(frp.NewManager(filepath.Join(dir, "bin"), dir), agenthub.New())
	r := ginRouter()
	r.POST("/api/servers/:id/stop", h.Stop)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/servers/1/stop", nil))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var saved model.FrpsServer
	database.DB.First(&saved, server.ID)
	if !saved.ManualStopped {
		t.Fatal("停止意图没有保存")
	}
	var n int64
	database.DB.Model(&model.ConfigVersion{}).Count(&n)
	if n != 0 {
		t.Fatal("停止生成了配置快照")
	}
}

func TestRollbackCommandContainsPanelSnapshot(t *testing.T) {
	securityDB(t)
	agent := model.Agent{Name: "owner", NodeKey: "key", Secret: "secret", Roles: "frpc"}
	database.DB.Create(&agent)
	node := model.Node{Name: "node", NodeKey: "node-key", AgentID: agent.ID}
	database.DB.Create(&node)
	content := `{"serverAddr":"panel.example","serverPort":7000}`
	database.DB.Create(&model.ConfigVersion{TargetType: "node", TargetID: node.ID, Version: 1, Content: content, Status: "applied"})
	r := ginRouter()
	r.POST("/api/nodes/:id/rollback", NewNodeHandler(agenthub.New(), nil).Rollback)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/nodes/1/rollback", strings.NewReader(`{"version":1}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var cmd model.AgentCommand
	database.DB.First(&cmd)
	var payload struct {
		TargetVersion int    `json:"targetVersion"`
		Content       string `json:"content"`
	}
	if json.Unmarshal([]byte(cmd.Payload), &payload) != nil || payload.TargetVersion != 1 || payload.Content != content {
		t.Fatal("回滚仍依赖远端本地快照")
	}
}
