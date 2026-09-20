package model

import "time"

// Proxy 隧道/代理配置（归属于某个节点）
type Proxy struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	NodeID uint   `gorm:"index" json:"nodeId"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Type   string `gorm:"size:16;not null" json:"type"` // tcp/udp/http/https/stcp/sudp/xtcp

	LocalIP   string `gorm:"size:64;default:127.0.0.1" json:"localIp"`
	LocalPort int    `json:"localPort"`

	RemotePort    int    `json:"remotePort"`
	CustomDomains string `gorm:"size:255" json:"customDomains"`
	Subdomain     string `gorm:"size:128" json:"subdomain"`

	UseEncryption  bool `json:"useEncryption"`
	UseCompression bool `json:"useCompression"`
	GroupName      string `gorm:"size:64" json:"group"`

	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
