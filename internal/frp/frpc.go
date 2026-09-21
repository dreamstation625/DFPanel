package frp

// 本文件定义 frpc 客户端配置结构，字段对齐官方客户端配置文档：
// https://gofrp.org/zh-cn/docs/reference/client-configures/
// 以及通用配置文档：https://gofrp.org/zh-cn/docs/reference/common/
//
// M2 的 Agent 会用这里的 FrpcConfig 生成 frpc.json 下发给节点。

// AuthClientConfig 客户端鉴权配置（auth）
type AuthClientConfig struct {
	Method           string                `json:"method,omitempty"` // token / oidc，frp 默认 token
	AdditionalScopes []string              `json:"additionalScopes,omitempty"`
	Token            string                `json:"token,omitempty"`
	// TokenSource 与 Token 互斥：把 token 的来源交给文件或命令
	TokenSource *ValueSource          `json:"tokenSource,omitempty"`
	OIDC        *AuthOIDCClientConfig `json:"oidc,omitempty"`
}

// AuthOIDCClientConfig auth.method = oidc 时生效
type AuthOIDCClientConfig struct {
	ClientID                 string            `json:"clientID,omitempty"`
	ClientSecret             string            `json:"clientSecret,omitempty"`
	Audience                 string            `json:"audience,omitempty"`
	Scope                    string            `json:"scope,omitempty"`
	TokenEndpointURL         string            `json:"tokenEndpointURL,omitempty"`
	AdditionalEndpointParams map[string]string `json:"additionalEndpointParams,omitempty"`
	TrustedCaFile            string            `json:"trustedCaFile,omitempty"`
	InsecureSkipVerify       bool              `json:"insecureSkipVerify,omitempty"`
	ProxyURL                 string            `json:"proxyURL,omitempty"`
}

// TLSClientConfig 客户端 TLS 配置（transport.tls）
type TLSClientConfig struct {
	Enable                    *bool `json:"enable,omitempty"`                    // frp 默认 true
	DisableCustomTLSFirstByte *bool `json:"disableCustomTLSFirstByte,omitempty"` // frp 默认 true
	TLSConfig
}

// ClientTransportConfig 客户端网络层配置（transport）
type ClientTransportConfig struct {
	Protocol                string           `json:"protocol,omitempty"` // tcp / kcp / quic / websocket / wss
	WireProtocol            string           `json:"wireProtocol,omitempty"`
	DialServerTimeout       int              `json:"dialServerTimeout,omitempty"`
	DialServerKeepAlive     int              `json:"dialServerKeepalive,omitempty"`
	ConnectServerLocalIP    string           `json:"connectServerLocalIP,omitempty"`
	ProxyURL                string           `json:"proxyURL,omitempty"`
	PoolCount               int              `json:"poolCount,omitempty"`
	TCPMux                  *bool            `json:"tcpMux,omitempty"`
	TCPMuxKeepaliveInterval int              `json:"tcpMuxKeepaliveInterval,omitempty"`
	QUIC                    *QUICOptions     `json:"quic,omitempty"`
	HeartbeatInterval       int              `json:"heartbeatInterval,omitempty"`
	HeartbeatTimeout        int              `json:"heartbeatTimeout,omitempty"`
	TLS                     *TLSClientConfig `json:"tls,omitempty"`
}

// VirtualNetConfig 虚拟网络（Alpha）
type VirtualNetConfig struct {
	Address string `json:"address,omitempty"`
}

// StoreConfig 内置持久化存储，用于运行时动态管理代理
type StoreConfig struct {
	Path string `json:"path,omitempty"`
}

// ClientCommonConfig frpc 通用配置
type ClientCommonConfig struct {
	Auth AuthClientConfig `json:"auth,omitempty"`
	// User 代理名前缀，设置后代理名会变为 {user}.{proxyName}
	User string `json:"user,omitempty"`
	// ClientID 唯一标识本 frpc 实例（frp v0.67+）
	ClientID string `json:"clientID,omitempty"`

	ServerAddr string `json:"serverAddr,omitempty"`
	ServerPort int    `json:"serverPort,omitempty"`

	NatHoleSTUNServer string `json:"natHoleStunServer,omitempty"`
	DNSServer         string `json:"dnsServer,omitempty"`

	LoginFailExit *bool    `json:"loginFailExit,omitempty"` // frp 默认 true
	Start         []string `json:"start,omitempty"`

	Log        *LogConfig             `json:"log,omitempty"`
	WebServer  *WebServerConfig       `json:"webServer,omitempty"`
	Transport  *ClientTransportConfig `json:"transport,omitempty"`
	VirtualNet *VirtualNetConfig      `json:"virtualNet,omitempty"`

	FeatureGates map[string]bool   `json:"featureGates,omitempty"`
	Metadatas    map[string]string `json:"metadatas,omitempty"`

	UDPPacketSize      int          `json:"udpPacketSize,omitempty"`
	IncludeConfigFiles []string     `json:"includes,omitempty"`
	Store              *StoreConfig `json:"store,omitempty"`
}

// FrpcConfig 与 frpc.json 一一对应
// proxies / visitors 的字段随代理类型而不同，M3 落地代理配置后再结构化为具体类型，
// 现阶段用自由对象承载，避免错误地把参数写进未知字段（frp 对未知字段会严格报错）。
type FrpcConfig struct {
	ClientCommonConfig

	Proxies  []map[string]any `json:"proxies,omitempty"`
	Visitors []map[string]any `json:"visitors,omitempty"`
}

// BuildFrpcJSON 序列化 frpc.json
func BuildFrpcJSON(cfg *FrpcConfig) (string, error) {
	return renderWithExtra(cfg, "")
}
