package config

import (
	"flag"
	"os"
	"path/filepath"
)

// Config 面板全局配置
type Config struct {
	Listen         string // 面板监听地址，如 :8080
	DataDir        string // 数据目录（sqlite、frps 配置与日志）
	JWTSecret      string // JWT 签名密钥
	TokenExpireHrs int    // 登录有效期（小时）
	PublicURL      string // 面板对外访问地址，用于生成 Agent 安装命令与 frpc 连接地址
	AgentImage     string // 生成 Docker 安装命令时使用的 Agent 镜像
	Version        string // 面板版本号（构建时经 ldflags 注入，仅用于展示）
}

// DefaultAgentImage 默认 Agent 镜像（与 CI 推送的镜像保持一致）
const DefaultAgentImage = "dreamstation625/dfpanel-agent:latest"

// Load 加载配置，优先级：命令行参数 > 环境变量 > 默认值
func Load() *Config {
	listen := envOr("DFPANEL_LISTEN", ":8080")
	dataDir := envOr("DFPANEL_DATA_DIR", "./data")

	cfg := &Config{}
	flag.StringVar(&cfg.Listen, "listen", listen, "面板监听地址")
	flag.StringVar(&cfg.DataDir, "data", dataDir, "数据目录")
	flag.StringVar(&cfg.JWTSecret, "jwt-secret", os.Getenv("DFPANEL_JWT_SECRET"), "JWT 密钥（留空自动生成并持久化）")
	flag.IntVar(&cfg.TokenExpireHrs, "token-expire", 24, "登录有效期（小时）")
	flag.StringVar(&cfg.PublicURL, "public-url", envOr("DFPANEL_PUBLIC_URL", ""),
		"面板对外访问地址（如 http://1.2.3.4:8080），留空时按请求地址推断")
	flag.StringVar(&cfg.AgentImage, "agent-image", envOr("DFPANEL_AGENT_IMAGE", DefaultAgentImage),
		"生成 Docker 安装命令时使用的 Agent 镜像")
	flag.Parse()

	if abs, err := filepath.Abs(cfg.DataDir); err == nil {
		cfg.DataDir = abs
	}
	return cfg
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
