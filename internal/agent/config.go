// Package agent 实现远端 Agent：与面板通信，托管 frps 与 frpc 的启停、配置应用与失败回滚
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Config agent.json 结构；同名环境变量优先级更高，便于容器化部署
type Config struct {
	PanelURL   string `json:"panel_url"`
	NodeKey    string `json:"node_key"`
	Secret     string `json:"secret"`
	Roles      string `json:"roles"`     // 逗号分隔：frps / frpc，两者可并存
	Runtime    string `json:"runtime"`   // process = 直接起进程；docker = 起容器
	DataDir    string `json:"data_dir"`  // 配置与日志目录
	FRPVersion string `json:"frp_version"`
	// 容器化运行时使用的镜像，留空取默认值
	FrpsImage string `json:"frps_image"`
	FrpcImage string `json:"frpc_image"`
}

// DefaultImages 默认使用社区维护的 frp 官方镜像
const (
	DefaultFrpsImage = "snowdreamtech/frps:latest"
	DefaultFrpcImage = "snowdreamtech/frpc:latest"
)

// LoadConfig 读取配置文件，并用环境变量覆盖
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取配置失败：%w", err)
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置失败：%w", err)
		}
	}

	cfg.PanelURL = envOr("DFPANEL_URL", cfg.PanelURL)
	cfg.NodeKey = envOr("DFPANEL_NODE_KEY", cfg.NodeKey)
	cfg.Secret = envOr("DFPANEL_NODE_SECRET", cfg.Secret)
	cfg.Roles = envOr("DFPANEL_ROLES", cfg.Roles)
	cfg.Runtime = envOr("DFPANEL_RUNTIME", cfg.Runtime)
	cfg.DataDir = envOr("DFPANEL_DATA_DIR", cfg.DataDir)
	cfg.FrpsImage = envOr("DFPANEL_FRPS_IMAGE", cfg.FrpsImage)
	cfg.FrpcImage = envOr("DFPANEL_FRPC_IMAGE", cfg.FrpcImage)

	if cfg.Roles == "" {
		cfg.Roles = "frpc"
	}
	if cfg.Runtime == "" {
		cfg.Runtime = "process"
	}
	if cfg.FrpsImage == "" {
		cfg.FrpsImage = DefaultFrpsImage
	}
	if cfg.FrpcImage == "" {
		cfg.FrpcImage = DefaultFrpcImage
	}
	if cfg.DataDir == "" {
		cfg.DataDir = defaultDataDir()
	}
	if abs, err := filepath.Abs(cfg.DataDir); err == nil {
		cfg.DataDir = abs
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败：%w", err)
	}

	if cfg.PanelURL == "" || cfg.NodeKey == "" || cfg.Secret == "" {
		return nil, errors.New("缺少必填配置：panel_url / node_key / secret")
	}
	cfg.PanelURL = strings.TrimRight(cfg.PanelURL, "/")
	return cfg, nil
}

// HasRole 判断是否承担指定角色
func (c *Config) HasRole(role string) bool {
	for _, r := range strings.Split(c.Roles, ",") {
		if strings.TrimSpace(r) == role {
			return true
		}
	}
	return false
}

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "dfpanel-agent", "data")
	}
	return "/var/lib/dfpanel-agent"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Target 一个被托管对象：frps 服务端或 frpc 节点
type Target struct {
	Type string // server / node
	ID   uint
}

// ConfigPath 配置文件路径
func (c *Config) ConfigPath(t Target) string {
	kind := "frpc"
	if t.Type == "server" {
		kind = "frps"
	}
	return filepath.Join(c.DataDir, fmt.Sprintf("%s-%d.json", kind, t.ID))
}

// BackupPath 上一份可用配置（回滚依据）
func (c *Config) BackupPath(t Target) string {
	return c.ConfigPath(t) + ".bak"
}

// LogPath 进程日志路径（docker 运行时不使用，日志走 docker logs）
func (c *Config) LogPath(t Target) string {
	kind := "frpc"
	if t.Type == "server" {
		kind = "frps"
	}
	return filepath.Join(c.DataDir, fmt.Sprintf("%s-%d.log", kind, t.ID))
}

// BinaryPath frp 二进制路径：优先使用数据目录中的副本，其次回退到 PATH（容器镜像内置 / 系统安装）
func (c *Config) BinaryPath(kind string) string {
	name := kind
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	local := filepath.Join(c.DataDir, name)
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if p, err := exec.LookPath(kind); err == nil {
		return p
	}
	return local
}

// ContainerName 容器名（docker 运行时）
func ContainerName(kind string, id uint) string {
	return fmt.Sprintf("dfpanel-%s-%d", kind, id)
}
