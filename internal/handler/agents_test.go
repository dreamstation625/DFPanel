package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
)

// 编辑 Agent 的表单没有版本字段，保存时必须保留心跳上报和版本管理维护的值。
func TestUpdateAgentKeepsFrpState(t *testing.T) {
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

	agent := model.Agent{
		Name: "旧名称", NodeKey: "test-key", Secret: "test-secret", Roles: "frpc",
		Runtime: "docker", FRPVersion: "0.71.0", FRPInstalledVersion: "0.71.0",
		FRPCachedVersions: "0.71.0", Version: "0.0.1-beta.16",
	}
	if err := db.Create(&agent).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.PUT("/agents/:id", NewAgentManageHandler(nil).Update)
	request := httptest.NewRequest(http.MethodPut, "/agents/1", strings.NewReader(`{"name":"新名称","remark":"测试","roles":"frpc"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("编辑失败：status=%d body=%s", response.Code, response.Body.String())
	}

	var saved model.Agent
	if err := db.First(&saved, agent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Name != "新名称" || saved.Runtime != "docker" || saved.FRPVersion != "0.71.0" ||
		saved.FRPInstalledVersion != "0.71.0" || saved.FRPCachedVersions != "0.71.0" || saved.Version != "0.0.1-beta.16" {
		t.Fatalf("编辑 Agent 清掉了版本或运行状态：%+v", saved)
	}
}

func TestAgentTypesAndOneToOneBindings(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "binding.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Agent{}, &model.Node{}, &model.FrpsServer{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previousDB; _ = sqlDB.Close() })

	client := model.Agent{Name: "客户端", NodeKey: "client-key", Secret: "secret", Roles: "frpc"}
	server := model.Agent{Name: "服务端", NodeKey: "server-key", Secret: "secret", Roles: "frps", HostID: "shared-host"}
	for _, agent := range []*model.Agent{&client, &server} {
		if err := db.Create(agent).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := requireManagedAgent(client.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := requireManagedAgent(server.ID, 0); err == nil {
		t.Fatal("服务端 Agent 不应绑定客户端节点")
	}
	if err := requireServerAgent(&model.FrpsServer{DeployMode: "agent", AgentID: server.ID}, 0); err != nil {
		t.Fatal(err)
	}
	if err := requireServerAgent(&model.FrpsServer{DeployMode: "agent", AgentID: client.ID}, 0); err == nil {
		t.Fatal("客户端 Agent 不应绑定服务端")
	}
	node := model.Node{Name: "节点", NodeKey: "node-key", AgentID: client.ID}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := requireManagedAgent(client.ID, 0); err == nil {
		t.Fatal("客户端 Agent 不应重复绑定")
	}
	if err := requireManagedAgent(client.ID, node.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Node{Name: "重复节点", NodeKey: "other-key", AgentID: client.ID}).Error; err == nil {
		t.Fatal("数据库必须阻止并发重复绑定客户端 Agent")
	}
	frps := model.FrpsServer{Name: "服务端", DeployMode: "agent", AgentID: server.ID, BindPort: 7000}
	if err := db.Create(&frps).Error; err != nil {
		t.Fatal(err)
	}
	if err := requireServerAgent(&model.FrpsServer{DeployMode: "agent", AgentID: server.ID}, 0); err == nil {
		t.Fatal("服务端 Agent 不应重复绑定")
	}
	if err := db.Create(&model.FrpsServer{Name: "重复服务端", DeployMode: "agent", AgentID: server.ID}).Error; err == nil {
		t.Fatal("数据库必须阻止并发重复绑定服务端 Agent")
	}
	secondServerAgent := model.Agent{Name: "同机另一个服务端 Agent", NodeKey: "server-2-key", Secret: "secret", Roles: "frps", HostID: "shared-host"}
	if err := db.Create(&secondServerAgent).Error; err != nil {
		t.Fatal(err)
	}
	if err := checkPortConflict(&model.FrpsServer{DeployMode: "agent", AgentID: secondServerAgent.ID, BindPort: 7000}, 0); err == nil {
		t.Fatal("同机不同 Agent 不得使用相同监听端口")
	}
}
