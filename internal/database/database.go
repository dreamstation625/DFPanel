package database

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"dfpanel/internal/model"
)

// DB 全局数据库句柄
var DB *gorm.DB

// Init 初始化 SQLite（纯 Go 驱动，无需 CGO）并自动迁移表结构
func Init(dataDir string) error {
	dsn := filepath.Join(dataDir, "dfpanel.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	// 旧库中若已有同号快照，不能猜测哪个版本是回滚目标；拒绝迁移并保留全部数据。
	if db.Migrator().HasTable(&model.ConfigVersion{}) {
		var duplicate struct {
			TargetType string
			TargetID   uint
			Version    int
			N          int
		}
		if err := db.Raw("SELECT target_type,target_id,version,COUNT(*) AS n FROM config_versions GROUP BY target_type,target_id,version HAVING COUNT(*) > 1 LIMIT 1").Scan(&duplicate).Error; err != nil {
			sqlDB.Close()
			return err
		}
		if duplicate.N > 1 {
			sqlDB.Close()
			return fmt.Errorf("存在同号配置快照 %s/%d v%d，请先备份并核对重复记录，未自动删除或重编号", duplicate.TargetType, duplicate.TargetID, duplicate.Version)
		}
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.AuthSession{},
		&model.AgentNonce{},
		&model.FrpsServer{},
		&model.Node{},
		&model.Proxy{},
		&model.Visitor{},
		&model.Agent{},
		&model.ConfigVersion{},
		&model.AgentCommand{},
		&model.Setting{},
	); err != nil {
		sqlDB.Close()
		return err
	}
	// 旧 Agent 未保存执行收据，已发送但未确认的命令不能在升级后盲目重放。
	err = db.Transaction(func(tx *gorm.DB) error {
		var legacy []model.AgentCommand
		if err := tx.Where("status = ? AND delivery_version = ?", "sent", 0).Find(&legacy).Error; err != nil {
			return err
		}
		for _, cmd := range legacy {
			if cmd.Type == "unassign" {
				if err := tx.Model(&cmd).Updates(map[string]any{"status": "pending", "sent_at": nil}).Error; err != nil {
					return err
				}
				continue
			}
			message := "升级前命令的执行结果未确认，请核实实例后重新操作"
			if err := tx.Model(&cmd).Updates(map[string]any{"status": "failed", "result": message, "done_at": time.Now()}).Error; err != nil {
				return err
			}
			if cmd.Type == "apply" || cmd.Type == "rollback" {
				if err := tx.Model(&model.ConfigVersion{}).Where("target_type = ? AND target_id = ? AND version = ? AND status = ?", cmd.TargetType, cmd.TargetID, cmd.Version, "pending").Updates(map[string]any{"status": "failed", "message": message}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		sqlDB.Close()
		return err
	}
	DB = db
	return nil
}
