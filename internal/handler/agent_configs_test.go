package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

func TestManagedConfigsOnlyReturnsOwnedAppliedVersions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "configs.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Agent{}, &model.Node{}, &model.FrpsServer{}, &model.ConfigVersion{}); err != nil {
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

	agent := model.Agent{Name: "owner", NodeKey: "owner-key", Secret: "owner-secret", Roles: "frpc,frps"}
	other := model.Agent{Name: "other", NodeKey: "other-key", Secret: "other-secret", Roles: "frpc,frps"}
	for _, item := range []*model.Agent{&agent, &other} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	node := model.Node{Name: "client", AgentID: agent.ID, NodeKey: "node-key", AutoStart: true, ManualStopped: true}
	otherNode := model.Node{Name: "other-client", AgentID: other.ID, NodeKey: "other-node-key"}
	neverApplied := model.Node{Name: "new-client", AgentID: agent.ID, NodeKey: "new-node-key"}
	for _, item := range []*model.Node{&node, &otherNode, &neverApplied} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	server := model.FrpsServer{Name: "server", AgentID: agent.ID, DeployMode: "agent", AutoStart: true}
	localServer := model.FrpsServer{Name: "local", AgentID: agent.ID, DeployMode: "local"}
	for _, item := range []*model.FrpsServer{&server, &localServer} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	versions := []model.ConfigVersion{
		{TargetType: proto.TargetNode, TargetID: node.ID, Version: 1, Content: `{"v":1}`, Status: "applied"},
		{TargetType: proto.TargetNode, TargetID: node.ID, Version: 2, Content: `{"v":2}`, Status: "applied"},
		{TargetType: proto.TargetNode, TargetID: node.ID, Version: 3, Content: `{"v":3}`, Status: "failed"},
		{TargetType: proto.TargetNode, TargetID: neverApplied.ID, Version: 1, Content: `{"draft":true}`, Status: "pending"},
		{TargetType: proto.TargetNode, TargetID: otherNode.ID, Version: 1, Content: `{"other":true}`, Status: "applied"},
		{TargetType: proto.TargetServer, TargetID: server.ID, Version: 4, Content: `{"bindPort":7000}`, Status: "applied"},
		{TargetType: proto.TargetServer, TargetID: localServer.ID, Version: 1, Content: `{"local":true}`, Status: "applied"},
	}
	for i := range versions {
		if err := db.Create(&versions[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	router := gin.New()
	router.GET("/api/agent/configs", NewAgentHandler(nil).ManagedConfigs)
	request := httptest.NewRequest(http.MethodGet, signedConfigsURL(agent), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("配置响应未禁止缓存：%q", response.Header().Get("Cache-Control"))
	}
	var body struct {
		Configs []proto.ManagedConfig `json:"configs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Configs) != 2 {
		t.Fatalf("应只返回自身已应用的 frpc/frps 配置：%+v", body.Configs)
	}
	gotNode, gotServer := body.Configs[0], body.Configs[1]
	if gotNode.TargetType != proto.TargetNode || gotNode.TargetID != node.ID || gotNode.Version != 2 ||
		gotNode.Content != `{"v":2}` || !gotNode.AutoStart || !gotNode.ManualStopped {
		t.Fatalf("客户端恢复快照不正确：%+v", gotNode)
	}
	if gotServer.TargetType != proto.TargetServer || gotServer.TargetID != server.ID || gotServer.Version != 4 ||
		gotServer.Content != `{"bindPort":7000}` || !gotServer.AutoStart || gotServer.ManualStopped {
		t.Fatalf("服务端恢复快照不正确：%+v", gotServer)
	}

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/agent/configs", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("未签名请求应被拒绝：status=%d", unauthorized.Code)
	}
}

func signedConfigsURL(agent model.Agent) string {
	ts := time.Now().Unix()
	query := url.Values{
		"nodeKey": {agent.NodeKey},
		"ts":      {strconv.FormatInt(ts, 10)},
		"sign":    {proto.Sign(agent.Secret, agent.NodeKey, ts)},
	}
	return "/api/agent/configs?" + query.Encode()
}

func TestQueuedStopReportPersistsManualStopped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "report.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Agent{}, &model.Node{}, &model.AgentCommand{}); err != nil {
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

	agent := model.Agent{Name: "owner", NodeKey: "owner-key", Secret: "owner-secret", Roles: "frpc"}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "client", NodeKey: "node-key", AgentID: agent.ID, AutoStart: true}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	cmd := model.AgentCommand{AgentID: agent.ID, Type: proto.CmdStop, TargetType: proto.TargetNode, TargetID: node.ID}
	if err := db.Create(&cmd).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.POST("/api/agent/report", NewAgentHandler(nil).Report)
	request := httptest.NewRequest(http.MethodPost, strings.Replace(signedConfigsURL(agent), "/configs?", "/report?", 1),
		strings.NewReader(`{"commandId":1,"ok":true,"message":"已停止"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", response.Code, response.Body.String())
	}
	var saved model.Node
	if err := db.First(&saved, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.ManualStopped {
		t.Fatal("排队的停止指令完成后未保存手动停止状态")
	}
}
