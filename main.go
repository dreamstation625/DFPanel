package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/router"
)

// version 由构建时注入：-ldflags "-X main.version=x.y.z"；未注入时为 dev
var version = "dev"

func main() {
	// -version 直接输出到 stdout，便于脚本解析（不带日志时间戳）
	if len(os.Args) > 1 && (os.Args[1] == "-version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("dfpanel %s\n", version)
		return
	}

	cfg := config.Load()
	cfg.Version = version

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}

	if cfg.JWTSecret == "" {
		cfg.JWTSecret = loadOrCreateSecret(filepath.Join(cfg.DataDir, ".jwt_secret"))
	}

	if err := database.Init(cfg.DataDir); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	r := router.Setup(cfg)

	log.Printf("DFPanel %s 已启动，请访问 http://localhost%s", version, cfg.Listen)
	if err := r.Run(cfg.Listen); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}

// loadOrCreateSecret 读取或生成持久化的 JWT 密钥
func loadOrCreateSecret(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return string(b)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("生成 JWT 密钥失败: %v", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		log.Printf("写入 JWT 密钥失败: %v", err)
	}
	return secret
}
