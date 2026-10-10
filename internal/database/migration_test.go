package database

import (
	"dfpanel/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacySentCommandsAreNotBlindlyRepeated(t *testing.T) {
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "dfpanel.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AgentCommand{}, &model.ConfigVersion{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := model.AgentCommand{AgentID: 1, Type: "apply", TargetType: "node", TargetID: 1, Version: 1, Status: "sent", SentAt: &now}
	cleanup := model.AgentCommand{AgentID: 1, Type: "unassign", TargetType: "node", TargetID: 1, Status: "sent", SentAt: &now}
	current := model.AgentCommand{AgentID: 1, Type: "stop", Status: "sent", SentAt: &now, DeliveryVersion: 2}
	db.Create(&old)
	db.Create(&cleanup)
	db.Create(&current)
	db.Create(&model.ConfigVersion{TargetType: "node", TargetID: 1, Version: 1, Status: "pending"})
	sqlDB, _ := db.DB()
	sqlDB.Close()
	previous := DB
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	migrated, _ := DB.DB()
	t.Cleanup(func() { migrated.Close(); DB = previous })
	var cmds []model.AgentCommand
	DB.Order("id").Find(&cmds)
	if len(cmds) != 3 || cmds[0].Status != "failed" || cmds[1].Status != "pending" || cmds[2].Status != "sent" {
		t.Fatalf("迁移状态不正确：%+v", cmds)
	}
	var cv model.ConfigVersion
	DB.First(&cv)
	if cv.Status != "failed" {
		t.Fatal("未标记旧命令的不确定结果")
	}
}
