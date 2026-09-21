package model

import (
	"time"

	"gorm.io/gorm"
)

// FrpsServer frps 服务端配置（一体化模式下同时用于进程管理）
type FrpsServer struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"size:64;not null" json:"name"`

	// 监听相关
	BindAddr      string `gorm:"size:64;default:0.0.0.0" json:"bindAddr"`
	BindPort      int    `gorm:"default:7000" json:"bindPort"`
	KCPBindPort   int    `json:"kcpBindPort"`
	QUICBindPort  int    `json:"quicBindPort"`
	ProxyBindAddr string `gorm:"size:64" json:"proxyBindAddr"`

	// 虚拟主机
	VhostHTTPPort    int    `json:"vhostHttpPort"`    // 0 表示不启用 HTTP 代理
	VhostHTTPTimeout int    `json:"vhostHttpTimeout"` // 秒，frp 默认 60
	VhostHTTPSPort   int    `json:"vhostHttpsPort"`
	SubdomainHost    string `gorm:"size:128" json:"subdomainHost"`
	Custom404Page    string `gorm:"size:255" json:"custom404Page"`

	// tcpmux；注意 frp 的字段拼写为 tcpmuxHTTPConnectPort（mux 小写）
	TCPMuxHTTPConnectPort int  `json:"tcpmuxHttpConnectPort"`
	TCPMuxPassthrough     bool `json:"tcpmuxPassthrough"`

	// 鉴权 auth
	AuthMethod           string `gorm:"size:32;default:token" json:"authMethod"` // token / oidc
	AuthToken            string `gorm:"size:128" json:"authToken"`
	AuthAdditionalScopes string `gorm:"size:64" json:"authAdditionalScopes"` // 逗号分隔：HeartBeats,NewWorkConns
	// tokenSource 与 authToken 互斥：把 token 交给文件或命令提供
	AuthTokenSourceType string `gorm:"size:16" json:"authTokenSourceType"` // file / exec
	AuthTokenSourcePath string `gorm:"size:255" json:"authTokenSourcePath"`
	// AuthHTTPPlugins 服务端插件，JSON 数组文本（name / addr / path / ops / tlsVerify）
	AuthHTTPPlugins string `gorm:"type:text" json:"authHttpPlugins"`
	// auth.method = oidc 时生效
	AuthOIDCIssuer          string `gorm:"size:255" json:"authOidcIssuer"`
	AuthOIDCAudience        string `gorm:"size:255" json:"authOidcAudience"`
	AuthOIDCSkipExpiryCheck bool   `json:"authOidcSkipExpiryCheck"`
	AuthOIDCSkipIssuerCheck bool   `json:"authOidcSkipIssuerCheck"`

	// Dashboard（面板读取客户端与流量用）；默认不启用，关闭时不向 frps.json 输出 webServer 段
	// 用指针区分「未指定」（nil，按不启用处理）与「显式开启」（true）
	DashboardEnabled     *bool  `gorm:"default:false" json:"dashboardEnabled"`
	DashboardAddr        string `gorm:"size:64" json:"dashboardAddr"` // 留空为 0.0.0.0
	DashboardPort        int    `json:"dashboardPort"`
	DashboardUser        string `gorm:"size:64" json:"dashboardUser"`
	DashboardPwd         string `gorm:"size:128" json:"dashboardPwd"`
	DashboardAssetsDir   string `gorm:"size:255" json:"dashboardAssetsDir"`
	DashboardPprofEnable bool   `json:"dashboardPprofEnable"`
	// Dashboard 启用 HTTPS 的证书（webServer.tls）
	DashboardTLSCertFile string `gorm:"size:255" json:"dashboardTlsCertFile"`
	DashboardTLSKeyFile  string `gorm:"size:255" json:"dashboardTlsKeyFile"`

	// 端口限制：如 "2000-3000,4000,5000-5010"
	AllowPorts        string `gorm:"size:255" json:"allowPorts"`
	MaxPortsPerClient int    `json:"maxPortsPerClient"`

	// 传输层 transport；TcpMux 用指针，避免显式 false 被数据库默认值覆盖
	TcpMux                  *bool `gorm:"default:true" json:"tcpMux"`
	TcpMuxKeepaliveInterval int   `json:"tcpMuxKeepaliveInterval"` // 秒，frp 默认 30
	TcpKeepalive            int   `json:"tcpKeepalive"`            // 秒，frp 默认 7200
	MaxPoolCount            int   `json:"maxPoolCount"`            // frp 默认 5
	HeartbeatTimeout        int   `json:"heartbeatTimeout"`        // 秒，tcpMux 开启时 frp 默认 -1
	// transport.quic
	QuicKeepalivePeriod    int `json:"quicKeepalivePeriod"`
	QuicMaxIdleTimeout     int `json:"quicMaxIdleTimeout"`
	QuicMaxIncomingStreams int `json:"quicMaxIncomingStreams"`
	// transport.tls
	TLSForce         bool   `json:"tlsForce"`
	TLSCertFile      string `gorm:"size:255" json:"tlsCertFile"`
	TLSKeyFile       string `gorm:"size:255" json:"tlsKeyFile"`
	TLSTrustedCaFile string `gorm:"size:255" json:"tlsTrustedCaFile"`
	TLSServerName    string `gorm:"size:128" json:"tlsServerName"`

	// 其他顶层项
	DetailedErrorsToClient          *bool `gorm:"default:true" json:"detailedErrorsToClient"` // frp 默认 true
	UserConnTimeout                 int   `json:"userConnTimeout"`                            // 秒，frp 默认 10
	UDPPacketSize                   int   `json:"udpPacketSize"`                              // frp 默认 1500
	NatHoleAnalysisDataReserveHours int   `json:"natholeAnalysisDataReserveHours"`            // 小时，frp 默认 168

	// SSH 隧道网关 sshTunnelGateway（frp v0.68+）；BindPort 为 0 表示不启用
	SSHGatewayBindPort              int    `json:"sshGatewayBindPort"`
	SSHGatewayPrivateKeyFile        string `gorm:"size:255" json:"sshGatewayPrivateKeyFile"`
	SSHGatewayAutoGenPrivateKeyPath string `gorm:"size:255" json:"sshGatewayAutoGenPrivateKeyPath"`
	SSHGatewayAuthorizedKeysFile    string `gorm:"size:255" json:"sshGatewayAuthorizedKeysFile"`

	// 日志
	LogLevel             string `gorm:"size:16;default:info" json:"logLevel"`
	LogMaxDays           int    `gorm:"default:7" json:"logMaxDays"`
	LogDisablePrintColor bool   `json:"logDisablePrintColor"`

	// 需同时启用 Dashboard 才会生效
	EnablePrometheus bool `json:"enablePrometheus"`

	// 额外 JSON 片段（顶层浅合并，可覆盖已生成字段）
	ExtraJSON string `gorm:"type:text" json:"extraJson"`

	// 部署模式：local = 面板本机一体化托管；agent = 由远端 Agent 托管
	DeployMode string `gorm:"size:16;default:local" json:"deployMode"`
	// AgentID 部署模式为 agent 时，指定由哪个 Agent 承载
	AgentID uint `gorm:"index" json:"agentId"`
	// PublicAddr frps 对外地址（frpc 连接用），留空时取面板访问地址
	PublicAddr string `gorm:"size:128" json:"publicAddr"`

	// 运行时状态：running / stopped / not_installed / offline
	Status    string    `gorm:"size:32;default:stopped" json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AfterFind 保证对外始终返回布尔值：数据库中为 NULL 时按各字段默认值处理
func (s *FrpsServer) AfterFind(_ *gorm.DB) error {
	if s.DashboardEnabled == nil {
		s.DashboardEnabled = boolPtr(false)
	}
	if s.TcpMux == nil {
		s.TcpMux = boolPtr(true)
	}
	if s.DetailedErrorsToClient == nil {
		s.DetailedErrorsToClient = boolPtr(true)
	}
	return nil
}

func boolPtr(v bool) *bool { return &v }
