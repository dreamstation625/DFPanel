package model

import "time"

// Proxy 隧道/代理配置（归属于某个节点）
//
// 字段名与 frp 的 `proxies[]` 一一对应，但**注意嵌套层级**：frp 的 ProxyBaseConfig 里
// transport（useEncryption / useCompression / proxyProtocolVersion / bandwidthLimit）
// 与 loadBalancer（group / groupKey）都是嵌套结构，由 internal/frp/frpc_build.go 负责还原。
// 写在顶层会被 frp 判为未知字段（严格模式下 frpc 直接拒绝加载）。
type Proxy struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	NodeID uint   `gorm:"index" json:"nodeId"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Type   string `gorm:"size:16;not null" json:"type"` // tcp/udp/http/https/stcp/sudp/xtcp/tcpmux

	LocalIP   string `gorm:"size:64;default:127.0.0.1" json:"localIp"`
	LocalPort int    `json:"localPort"`

	RemotePort    int    `json:"remotePort"`
	CustomDomains string `gorm:"size:255" json:"customDomains"`
	Subdomain     string `gorm:"size:128" json:"subdomain"`

	// ---- 传输层（生成时分别落到 transport.* / loadBalancer.* / 顶层）----
	UseEncryption        bool   `json:"useEncryption"`
	UseCompression       bool   `json:"useCompression"`
	ProxyProtocolVersion string `gorm:"size:8" json:"proxyProtocolVersion"` // v1 / v2，空表示不用
	BandwidthLimit       string `gorm:"size:32" json:"bandwidthLimit"`      // 如 1MB
	BandwidthLimitMode   string `gorm:"size:16" json:"bandwidthLimitMode"`  // client / server
	DisableAssistedAddrs bool   `json:"natTraversalDisableAddrs"`           // natTraversal.disableAssistedAddrs

	// ---- 负载均衡：多个同名代理分担流量 ----
	GroupName string `gorm:"size:64" json:"group"`

	// ---- 点对点隧道（stcp / sudp / xtcp）----
	SecretKey  string `gorm:"size:128" json:"secretKey"`
	AllowUsers string `gorm:"size:255" json:"allowUsers"` // 逗号分隔，* 表示允许所有用户

	// ---- http / https / tcpmux ----
	HTTPUser          string `gorm:"size:64" json:"httpUser"`
	HTTPPassword      string `gorm:"size:128" json:"httpPassword"`
	Locations         string `gorm:"size:255" json:"locations"` // 逗号分隔，仅 http
	HostHeaderRewrite string `gorm:"size:255" json:"hostHeaderRewrite"`
	RouteByHTTPUser   string `gorm:"size:64" json:"routeByHttpUser"`
	Multiplexer       string `gorm:"size:32" json:"multiplexer"` // tcpmux：httpconnect

	// ---- 健康检查：frpc 探测本地服务，连续失败则从 frps 摘除 ----
	HealthCheckType            string `gorm:"size:8" json:"healthCheckType"` // tcp / http
	HealthCheckTimeoutSeconds  int    `json:"healthCheckTimeoutSeconds"`
	HealthCheckMaxFailed       int    `json:"healthCheckMaxFailed"`
	HealthCheckIntervalSeconds int    `json:"healthCheckIntervalSeconds"`
	HealthCheckPath            string `gorm:"size:255" json:"healthCheckPath"` // http 类型时的探测路径

	// ---- 元信息 ----
	// 每行 key=value（也兼容逗号分隔），预览在 frps dashboard 上
	Annotations string `gorm:"type:text" json:"annotations"`
	// 传给服务端插件使用
	Metadatas string `gorm:"type:text" json:"metadatas"`

	// ---- 本地服务插件：配了插件后 localIP/localPort 不生效 ----
	// PluginType 为插件类型（unix_domain_socket / socks5 / static_file / https2http ...），
	// PluginConfig 为该插件其余参数的 JSON 对象（不同类型字段差异大，用 JSON 承载更实在）。
	PluginType   string `gorm:"size:32" json:"pluginType"`
	PluginConfig string `gorm:"type:text" json:"pluginConfig"`

	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsP2P 点对点隧道（需要访问端 visitor 才能连通）
func (p *Proxy) IsP2P() bool {
	switch p.Type {
	case "stcp", "sudp", "xtcp":
		return true
	}
	return false
}
