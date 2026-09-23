package model

import "time"

// Node 客户端节点（对应一台安装 Agent 的机器）
//
// 字段覆盖 frpc 的 ClientCommonConfig（含 auth / transport / transport.tls）。
// 注意 `TCPMux`、`LoginFailExit`、`DisableCustomTLSFirstByte` 用指针：
// frp 里它们有非零默认值（tcpMux / loginFailExit / disableCustomTLSFirstByte 默认均为 true），
// 只有指针才能区分「未设置（交给 frp 默认）」与「显式设为 false」。
type Node struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:64;not null" json:"name"`
	ServerID  uint   `gorm:"index" json:"serverId"`
	GroupName string `gorm:"size:64" json:"groupName"`
	Remark    string `gorm:"size:255" json:"remark"`

	// 安装令牌
	NodeKey string `gorm:"uniqueIndex;size:64;not null" json:"nodeKey"`
	Secret  string `gorm:"size:128;not null" json:"-"`

	// ---- 身份 ----
	// ClientID 唯一标识本 frpc 实例，用于 frps 侧区分重名客户端
	ClientID string `gorm:"size:64" json:"clientId"`
	// User 用户标识，设置后代理名会变为 {user}.{proxyName}
	User string `gorm:"size:64" json:"user"`

	// ---- 连接（serverAddr 留空则取所关联 frps 的公网地址）----
	ServerAddr string `gorm:"size:128" json:"serverAddr"`
	ServerPort int    `json:"serverPort"`
	// NatHoleSTUNServer STUN 服务器，留空使用 frp 内置默认
	NatHoleSTUNServer string `gorm:"size:128" json:"natHoleStunServer"`
	DNSServer         string `gorm:"size:64" json:"dnsServer"`
	// LoginFailExit 首次登录失败是否直接退出（frp 默认 true；false 表示持续重连）
	LoginFailExit *bool `json:"loginFailExit"`

	// ---- 鉴权：token 与 tokenSource 互斥；method=oidc 时用下面一组 ----
	AuthMethod               string `gorm:"size:16;default:token" json:"authMethod"` // token / oidc
	AuthToken                string `gorm:"size:128" json:"authToken"`
	AuthTokenSourceType      string `gorm:"size:16" json:"authTokenSourceType"` // file / exec，留空则用 AuthToken
	AuthTokenSourcePath      string `gorm:"size:255" json:"authTokenSourcePath"`
	AuthOIDCClientID         string `gorm:"size:128" json:"authOidcClientId"`
	AuthOIDCClientSecret     string `gorm:"size:255" json:"authOidcClientSecret"`
	AuthOIDCAudience         string `gorm:"size:255" json:"authOidcAudience"`
	AuthOIDCScope            string `gorm:"size:255" json:"authOidcScope"`
	AuthOIDCTokenEndpointURL string `gorm:"size:255" json:"authOidcTokenEndpointUrl"`
	// AuthOIDCAdditionalParams 附加请求参数，每行 key=value
	AuthOIDCAdditionalParams string `gorm:"type:text" json:"authOidcAdditionalParams"`

	// ---- transport ----
	// Protocol 支持 tcp / kcp / quic / websocket / wss，默认 tcp
	Protocol string `gorm:"size:16;default:tcp" json:"protocol"`
	// WireProtocol 链路协议 v1 / v2，留空为 v1
	WireProtocol string `gorm:"size:8" json:"wireProtocol"`
	// DialServerTimeout 连接服务端超时（秒）
	DialServerTimeout int `json:"dialServerTimeout"`
	// DialServerKeepAlive 连接服务端 keepalive（秒，负数表示禁用）
	DialServerKeepAlive int `json:"dialServerKeepalive"`
	// ConnectServerLocalIP 连接服务端时绑定的本机 IP（仅 tcp / websocket 生效）
	ConnectServerLocalIP string `gorm:"size:64" json:"connectServerLocalIp"`
	// ProxyURL 连接服务端走 http / socks5 / ntlm 代理（仅 tcp 生效）
	ProxyURL string `gorm:"size:255" json:"proxyUrl"`
	// PoolCount 预先建立到服务端的连接数
	PoolCount int `json:"poolCount"`
	// TCPMux 必须与服务端一致；指针以区分「未设置」与「显式关闭」
	TCPMux                  *bool `json:"tcpMux"`
	TCPMuxKeepaliveInterval int   `json:"tcpMuxKeepaliveInterval"`
	HeartbeatInterval       int   `json:"heartbeatInterval"`
	HeartbeatTimeout        int   `json:"heartbeatTimeout"`

	// ---- transport.tls ----
	// TLSEnable 是否与服务端启用 TLS。frp 默认 true，这里显式下发，保证界面上关掉真的能生效
	TLSEnable bool `json:"tlsEnable"`
	// DisableCustomTLSFirstByte 关闭 TLS 首字节（frp 默认 true）
	DisableCustomTLSFirstByte *bool  `json:"disableCustomTlsFirstByte"`
	TLSCertFile               string `gorm:"size:255" json:"tlsCertFile"`
	TLSKeyFile                string `gorm:"size:255" json:"tlsKeyFile"`
	TLSTrustedCaFile          string `gorm:"size:255" json:"tlsTrustedCaFile"`
	TLSServerName             string `gorm:"size:128" json:"tlsServerName"`

	// ---- 其他 ----
	// UDPPacketSize 需与服务端一致，影响 udp / sudp 代理
	UDPPacketSize int `json:"udpPacketSize"`
	// Metadatas 客户端元信息，每行 key=value
	Metadatas string `gorm:"type:text" json:"metadatas"`

	// AgentID 托管该节点 frpc 的 Agent（frpc 永远由 Agent 承载）
	AgentID uint `gorm:"index" json:"agentId"`

	// AutoStart Agent 重启后是否自动拉起该 frpc
	AutoStart bool `gorm:"default:false" json:"autoStart"`
	// ManualStopped 被手动停过：自动启动时跳过，手动启动或重启后清除
	ManualStopped bool `gorm:"default:false" json:"manualStopped"`

	// 运行时上报
	OS         string     `gorm:"size:32" json:"os"`
	Arch       string     `gorm:"size:32" json:"arch"`
	Version    string     `gorm:"size:32" json:"version"`
	Status     string     `gorm:"size:32;default:offline" json:"status"` // online / offline
	LastSeen   *time.Time `json:"lastSeen"`
	RemoteAddr string     `gorm:"size:64" json:"remoteAddr"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
