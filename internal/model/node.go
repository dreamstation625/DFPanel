package model

import "time"

// Node 客户端节点（对应一台安装 Agent 的机器）
type Node struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"size:64;not null" json:"name"`
	ServerID  uint   `gorm:"index" json:"serverId"`
	GroupName string `gorm:"size:64" json:"groupName"`
	Remark    string `gorm:"size:255" json:"remark"`

	// 安装令牌
	NodeKey string `gorm:"uniqueIndex;size:64;not null" json:"nodeKey"`
	Secret  string `gorm:"size:128;not null" json:"-"`

	// frpc 公共参数（serverAddr 留空则取所关联 frps 的公网地址）
	ServerAddr string `gorm:"size:128" json:"serverAddr"`
	ServerPort int    `json:"serverPort"`
	AuthToken  string `gorm:"size:128" json:"authToken"`
	TLSEnable  bool   `json:"tlsEnable"`
	Protocol   string `gorm:"size:16;default:tcp" json:"protocol"`
	User       string `gorm:"size:64" json:"user"`

	// AgentID 托管该节点 frpc 的 Agent（frpc 永远由 Agent 承载）
	AgentID uint `gorm:"index" json:"agentId"`

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
