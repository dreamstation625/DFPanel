package frp

// 本文件定义 frp 的「通用配置」结构，frps.json 与 frpc.json 共用。
//
// 参考文档：https://gofrp.org/zh-cn/docs/reference/common/
// 字段名与 json tag 严格对齐 frp 源码 pkg/config/v1/common.go。
// frp 官方结构里 log / webServer / transport 均为非指针结构，json tag 上的
// omitempty 对结构体无效，会输出空的 {} 对象；这里统一改用指针，让 omitempty 生效，
// 未配置的段落完全不出现在产物里。

// LogConfig 日志配置（log）
type LogConfig struct {
	// To 日志输出文件，"console" 表示标准输出，留空时 frp 默认 console
	To string `json:"to,omitempty"`
	// Level trace / debug / info / warn / error，frp 默认 info
	Level string `json:"level,omitempty"`
	// MaxDays 日志保留天数，frp 默认 3
	MaxDays int `json:"maxDays,omitempty"`
	// DisablePrintColor 禁用控制台日志颜色
	DisablePrintColor bool `json:"disablePrintColor,omitempty"`
}

// WebServerConfig Dashboard / AdminServer 配置（webServer）
type WebServerConfig struct {
	// Addr 监听地址，frps 默认为 0.0.0.0（端口 > 0 时），frpc 默认为 127.0.0.1
	Addr string `json:"addr,omitempty"`
	// Port 监听端口，0 表示不启动
	Port int `json:"port,omitempty"`
	// User / Password HTTP BasicAuth 凭据
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	// AssetsDir 自定义 Dashboard 静态资源目录
	AssetsDir string `json:"assetsDir,omitempty"`
	// PprofEnable 启用 Go pprof 调试接口
	PprofEnable bool `json:"pprofEnable,omitempty"`
	// TLS 启用 HTTPS 的证书配置
	TLS *TLSConfig `json:"tls,omitempty"`
}

// TLSConfig TLS 证书配置（四位通用字段）
type TLSConfig struct {
	CertFile      string `json:"certFile,omitempty"`
	KeyFile       string `json:"keyFile,omitempty"`
	TrustedCaFile string `json:"trustedCaFile,omitempty"`
	ServerName    string `json:"serverName,omitempty"`
}

// QUICOptions QUIC 协议参数（transport.quic）
type QUICOptions struct {
	KeepalivePeriod    int `json:"keepalivePeriod,omitempty"`    // 秒，默认 10
	MaxIdleTimeout     int `json:"maxIdleTimeout,omitempty"`     // 秒，默认 30
	MaxIncomingStreams int `json:"maxIncomingStreams,omitempty"` // 默认 100000
}

// PortRange 端口范围（allowPorts 的元素）
// 单一端口用 Single 表示，连续区间用 Start + End 表示。
type PortRange struct {
	Start  int `json:"start,omitempty"`
	End    int `json:"end,omitempty"`
	Single int `json:"single,omitempty"`
}

// ValueSource 值的来源（auth.tokenSource），用于把 token 从文件或命令里读出来，
// 与 auth.token 互斥。字段对齐 frp pkg/config/v1/value_source.go。
type ValueSource struct {
	Type string      `json:"type"` // file / exec
	File *FileSource `json:"file,omitempty"`
	Exec *ExecSource `json:"exec,omitempty"`
}

// FileSource 从文件读取
type FileSource struct {
	Path string `json:"path"`
}

// ExecSource 从命令读取
type ExecSource struct {
	Command string       `json:"command"`
	Args    []string     `json:"args,omitempty"`
	Env     []ExecEnvVar `json:"env,omitempty"`
}

// ExecEnvVar 执行命令时的环境变量
type ExecEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HTTPPluginOptions 服务端插件（frps 的 httpPlugins）
type HTTPPluginOptions struct {
	Name      string   `json:"name"`
	Addr      string   `json:"addr"`
	Path      string   `json:"path"`
	Ops       []string `json:"ops"`
	TLSVerify bool     `json:"tlsVerify,omitempty"`
}
