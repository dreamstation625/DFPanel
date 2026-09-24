package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

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
	// 部署脚本据此确认二进制支持 Windows 服务协议，避免安装旧 Release 后无法启动。
	if len(os.Args) > 1 && os.Args[1] == "-service-ready" {
		if runtime.GOOS != "windows" {
			os.Exit(1)
		}
		fmt.Println("DFPanel Windows service ready")
		return
	}

	cfg := config.Load()
	cfg.Version = version
	closeLog, err := preparePanelLog(cfg)
	if err != nil {
		log.Fatalf("初始化服务日志失败: %v", err)
	}
	defer closeLog()
	server := newPanelServer(cfg)
	if err := servePanel(server, cfg); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}

// newPanelServer 准备面板的持久化数据和 HTTP 服务；交互运行与 Windows 服务共用此入口。
func newPanelServer(cfg *config.Config) *http.Server {
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
	return &http.Server{Addr: cfg.Listen, Handler: r}
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
