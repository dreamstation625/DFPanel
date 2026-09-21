package model

import "time"

// Visitor 访问端（frpc 的 visitors）：点对点隧道（stcp / sudp / xtcp）的访问侧。
//
// 拓扑：`访问方 frpc(visitor) → frps → 服务方 frpc(stcp/xtcp 代理) → 本地服务`
// 因此只配隧道（proxies 里的 stcp/xtcp）只能把服务注册上去，必须再配一个 visitor
// 才能真正连通 —— 这个模型此前完全缺失。
type Visitor struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	NodeID uint   `gorm:"index" json:"nodeId"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Type   string `gorm:"size:16;not null" json:"type"` // stcp / sudp / xtcp

	// ServerName 要访问的服务方代理名（对应 proxy 的 name）
	ServerName string `gorm:"size:64;not null" json:"serverName"`
	// ServerUser 服务方所属用户 user，留空表示当前用户
	ServerUser string `gorm:"size:64" json:"serverUser"`
	// SecretKey 必须与服务方代理的 secretKey 一致
	SecretKey string `gorm:"size:128" json:"secretKey"`

	// BindAddr / BindPort 访问端本地监听的地址端口。
	// BindPort 为负数时表示不监听端口，只接受其它 visitor 转发过来的连接（sudp 不支持）。
	BindAddr string `gorm:"size:64;default:127.0.0.1" json:"bindAddr"`
	BindPort int    `json:"bindPort"`

	// 访问端与 frps 之间的传输加密 / 压缩（对应 visitors[].transport）
	UseEncryption  bool `json:"useEncryption"`
	UseCompression bool `json:"useCompression"`

	// ---- 仅 xtcp 生效：自动保持隧道 ----
	KeepTunnelOpen    bool `json:"keepTunnelOpen"`
	MaxRetriesAnHour  int  `json:"maxRetriesAnHour"`
	MinRetryInterval  int  `json:"minRetryInterval"`
	FallbackTimeoutMs int  `json:"fallbackTimeoutMs"`
	// FallbackTo 打洞失败时回退到的 visitor 名称
	FallbackTo string `gorm:"size:64" json:"fallbackTo"`

	// 禁用本地网卡辅助地址（网络环境差时可提升打洞效果）
	DisableAssistedAddrs bool `json:"natTraversalDisableAddrs"`

	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
