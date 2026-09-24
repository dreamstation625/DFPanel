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

	"dfpanel/internal/distrib"
)

// Config agent.json 结构；同名环境变量优先级更高，便于容器化部署
type Config struct {
	PanelURL   string `json:"panel_url"`
	NodeKey    string `json:"node_key"`
	Secret     string `json:"secret"`
	Roles      string `json:"roles"`     // 逗号分隔：frps / frpc，两者可并存
	Runtime    string `json:"runtime"`   // process = 直接起进程；docker = 起容器
	DataDir    string `json:"data_dir"` // 配置与日志目录
	// HostDataDir 宿主机上的数据目录，仅 runtime=docker 且 Agent 自身在容器里时需要。
	//
	// frp 容器由宿主机 docker daemon 创建，bind-mount 的源路径必须是宿主路径：
	// 容器里的 /var/lib/dfpanel-agent 在宿主上不存在，直接挂会让 docker 建出一个空目录，
	// 容器里就变成 exec "/dfpanel-frpc": is a directory。
	HostDataDir string `json:"host_data_dir"`
	FRPVersion  string `json:"frp_version"`
	// 容器化运行时使用的镜像，留空取默认值
	FrpsImage string `json:"frps_image"`
	FrpcImage string `json:"frpc_image"`
}

// DefaultImages docker 运行时的容器底座镜像。
//
// 底座里刻意不带 frp：二进制一律由面板下发并挂载进容器（见 Spec.MountBinary），
// 这样版本才完全受控，不会出现「容器跑的是镜像里那个版本不明的 frp」的情况。
// alpine 只为提供一个能跑静态二进制的极简环境，可用 DFPANEL_FRPS_IMAGE / DFPANEL_FRPC_IMAGE 覆盖。
const (
	DefaultFrpsImage = "alpine:3.20"
	DefaultFrpcImage = "alpine:3.20"
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
	cfg.HostDataDir = envOr("DFPANEL_HOST_DATA_DIR", cfg.HostDataDir)
	cfg.FRPVersion = envOr("DFPANEL_FRPVERSION", cfg.FRPVersion)
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

// PidPath 子进程 pid 文件路径：Agent 重启后据此接管仍在运行的实例
func (c *Config) PidPath(t Target) string {
	kind := "frpc"
	if t.Type == "server" {
		kind = "frps"
	}
	return filepath.Join(c.DataDir, fmt.Sprintf("%s-%d.pid", kind, t.ID))
}

// BinaryPath frp 二进制路径：优先使用数据目录中的副本，其次回退到 PATH（容器镜像内置 / 系统安装）
//
// 这是 active 槽位，路径字面固定。版本切换只替换该路径指向的内容，
// 因此 Spec.BinPath 等持有该路径的缓存无需重建。
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

// LocalBinaryPath active 槽位的本地路径（<dataDir>/frps[.exe]），不回落 PATH。
// 版本切换只允许写这个位置，避免去改系统的 /usr/local/bin。
func (c *Config) LocalBinaryPath(kind string) string {
	name := kind
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(c.DataDir, name)
}

// BinDir 版本化二进制存储目录：<dataDir>/bin，多版本共存、互不覆盖
func (c *Config) BinDir() string {
	return filepath.Join(c.DataDir, "bin")
}

// VersionSidecar 记录 active 槽位版本的落签文件路径
func (c *Config) VersionSidecar(kind string) string {
	return filepath.Join(c.BinDir(), kind+".version")
}

// VersionedBinaryPath 某个具体版本的二进制路径
func (c *Config) VersionedBinaryPath(kind, version string) string {
	return filepath.Join(c.BinDir(), distrib.BinaryName(kind, version, runtime.GOOS, runtime.GOARCH))
}

// BinaryVersion 查询 active 槽位当前的 frp 版本
func (c *Config) BinaryVersion(kind string) string {
	return distrib.ActiveVersion(c.BinaryPath(kind), c.VersionSidecar(kind))
}

// CachedVersions 列出本地已缓存的该 kind 的版本
func (c *Config) CachedVersions(kind string) []string {
	return distrib.CachedVersions(c.BinDir(), kind, runtime.GOOS, runtime.GOARCH)
}

// 容器内固定挂载点。刻意用单层路径：Docker 挂载单个文件时不需要创建多级父目录。
const (
	ContainerFrpsPath = "/dfpanel-frps"
	ContainerFrpcPath = "/dfpanel-frpc"
)

// ContainerBinaryInContainer 容器内用于运行 frp 的路径（底座镜像里没有 frp，与它无关）
func ContainerBinaryInContainer(kind string) string {
	if kind == "frps" {
		return ContainerFrpsPath
	}
	return ContainerFrpcPath
}

// ContainerSlotPath docker 运行时的 active 槽位：<binDir>/<kind>-container-<os>-<arch>
//
// 与 process 模式的槽位（<dataDir>/<kind>[.exe]）刻意分开：
// 同一台机器上两种运行时可并存，且 Windows 宿主上 process 槽位带 .exe 后缀，
// 而给容器的必须是 linux 二进制、不能带后缀。
func (c *Config) ContainerSlotPath(kind, goos, goarch string) string {
	return filepath.Join(c.BinDir(), fmt.Sprintf("%s-container-%s-%s", kind, goos, goarch))
}

// ContainerVersionSidecar 容器槽位的版本落签文件
func (c *Config) ContainerVersionSidecar(kind, goos, goarch string) string {
	return filepath.Join(c.BinDir(), fmt.Sprintf("%s-container-%s-%s.version", kind, goos, goarch))
}

// ContainerSlotVersion 容器槽位当前生效的 frp 版本（空表示面板还没下发过二进制）
func (c *Config) ContainerSlotVersion(kind, goos, goarch string) string {
	return distrib.ActiveVersion(c.ContainerSlotPath(kind, goos, goarch), c.ContainerVersionSidecar(kind, goos, goarch))
}

// ContainerCachedVersions 该容器平台已缓存的版本
func (c *Config) ContainerCachedVersions(kind, goos, goarch string) []string {
	return distrib.CachedVersions(c.BinDir(), kind, goos, goarch)
}

// ContainerName 容器名（docker 运行时）
func ContainerName(kind string, id uint) string {
	return fmt.Sprintf("dfpanel-%s-%d", kind, id)
}

// HostPath 把「Agent 自己看到的路径」翻译成宿主机路径。
//
// runtime=docker 时 frp 容器由宿主的 docker daemon 创建，bind-mount 的源路径必须是宿主路径：
//   - 配了 HostDataDir：按 DataDir → HostDataDir 换算（Agent 直装在宿主上时它为空，原样返回）
//   - 没配：反查自己这个容器的挂载映射 —— compose 里写相对路径（./agent-data）或命名卷时，
//     宿主的绝对路径只有 daemon 知道，配置里写不出来
func (c *Config) HostPath(p string) string {
	if p == "" {
		return p
	}
	if c.HostDataDir != "" {
		return rewritePrefix(p, c.DataDir, c.HostDataDir)
	}
	if c.Runtime != "docker" || !InContainer() {
		return p
	}
	return hostPathByMounts(p, cachedHostMounts())
}

// HostPathError 检查容器内路径到底能不能翻译成宿主路径。
//
// 翻译不了就别启动 frp 容器：挂载源是 Agent 视角的路径时，宿主上不存在，
// docker 会按惯例把它建成一个空目录，最后报一句难懂的 is a directory。
func (c *Config) HostPathError() error {
	if c.Runtime != "docker" || !InContainer() {
		return nil
	}
	return hostPathProblem(c.DataDir, c.HostDataDir, cachedHostMounts())
}

// hostPathProblem 纯函数版本，便于单测
func hostPathProblem(dataDir, hostDataDir string, mounts map[string]string) error {
	if hostDataDir != "" {
		return nil
	}
	if len(mounts) == 0 {
		return errors.New("Agent 自身跑在容器里，但既没配 DFPANEL_HOST_DATA_DIR，" +
			"也没能反查到自己容器的挂载映射（docker inspect 失败，或容器名/hostname 不是 dfpanel-agent）：" +
			"frp 容器挂载用的路径在宿主上不存在，docker 会把它建成空目录")
	}
	if hostPathByMounts(dataDir, mounts) == dataDir {
		return fmt.Errorf("Agent 自身跑在容器里，但数据目录 %s 不在任何挂载点下，翻译不出宿主路径："+
			"请把数据目录挂出来（如 ./dfpanel-agent-data:/var/lib/dfpanel-agent），或显式设置 DFPANEL_HOST_DATA_DIR", dataDir)
	}
	return nil
}
