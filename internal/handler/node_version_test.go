package handler

import (
	"encoding/json"
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
	"dfpanel/internal/proto"
)

// 历史配置要按节点和版本精确读取；列表只暴露是否有正文，不批量返回密钥。
func TestNodeVersionConfigReturnsSavedSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "versions.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Node{}, &model.ConfigVersion{}); err != nil {
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

	node := model.Node{Name: "节点一", NodeKey: "node-one", Secret: "secret-one"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	other := model.Node{Name: "节点二", NodeKey: "node-two", Secret: "secret-two"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	content := `{"serverAddr":"old.example","auth":{"token":"version-one-secret"}}`
	for _, item := range []model.ConfigVersion{
		{TargetType: proto.TargetNode, TargetID: node.ID, Version: 1, Content: content},
		{TargetType: proto.TargetNode, TargetID: node.ID, Version: 2, Content: `{"serverAddr":"new.example"}`},
		{TargetType: proto.TargetNode, TargetID: other.ID, Version: 3, Content: `{"serverAddr":"other.example"}`},
	} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatal(err)
		}
	}

	router := gin.New()
	router.GET("/nodes/:id/versions/:version/config", NewNodeHandler(nil, nil).VersionConfig)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/nodes/1/versions/1/config", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("读取历史配置失败：status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Content != content {
		t.Fatalf("未返回指定版本的原始配置：%q, %v", body.Content, err)
	}
	versions := loadVersionRecords(proto.TargetNode, node.ID)
	if len(versions) != 2 || !versions[0].HasConfig || !versions[1].HasConfig {
		t.Fatalf("历史列表未标记配置可查看：%+v", versions)
	}
	listJSON, err := json.Marshal(versions)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(listJSON), "version-one-secret") {
		t.Fatal("历史列表不应批量返回配置中的密钥")
	}
	merged := mergeVersions([]proto.HistoryEntry{{Version: 5}}, versions)
	if len(merged) == 0 || !merged[0].HasConfig || !merged[0].OnAgent {
		t.Fatalf("Agent 独有的快照应允许读取：%+v", merged)
	}

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/nodes/1/versions/3/config", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("不应读取其他节点的版本：status=%d", response.Code)
	}
}
