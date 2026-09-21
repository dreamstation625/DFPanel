package database

import (
	"path/filepath"

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
	DB = db
	return db.AutoMigrate(
		&model.User{},
		&model.FrpsServer{},
		&model.Node{},
		&model.Proxy{},
		&model.Visitor{},
		&model.Agent{},
		&model.ConfigVersion{},
		&model.AgentCommand{},
		&model.Setting{},
	)
}
