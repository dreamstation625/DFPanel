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
