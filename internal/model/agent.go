package model

import (
	"strings"
	"time"
)

// Agent 部署在远端机器上的守护程序（单一二进制，可同时托管 frps 与 frpc）
type Agent struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Remark string `gorm:"size:255" json:"remark"`

	// NodeKey 安装令牌（公开），Secret 签名密钥（不出网，仅安装命令中出现一次）
	NodeKey string `gorm:"uniqueIndex;size:64;not null" json:"nodeKey"`
	Secret  string `gorm:"size:128;not null" json:"secret"`

	// Roles 该 Agent 允许承载的角色，逗号分隔：frps / frpc，可同时具备
	Roles string `gorm:"size:64;default:frpc" json:"roles"`

	// 运行时上报
	OS         string     `gorm:"size:32" json:"os"`
	Arch       string     `gorm:"size:32" json:"arch"`
	Hostname   string     `gorm:"size:128" json:"hostname"`
	Version    string     `gorm:"size:32" json:"version"`
	Status     string     `gorm:"size:32;default:offline" json:"status"` // online / offline
	LastSeen   *time.Time `json:"lastSeen"`
	RemoteAddr string     `gorm:"size:64" json:"remoteAddr"`
	LastError  string     `gorm:"size:512" json:"lastError"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// HasRole 判断是否具备某个角色（frps / frpc）
func (a *Agent) HasRole(role string) bool {
	for _, r := range strings.Split(a.Roles, ",") {
		if strings.TrimSpace(r) == strings.TrimSpace(role) {
			return true
		}
	}
	return false
}

// ConfigVersion 每次下发配置的留痕，是 Agent 回滚与面板「一键回退」的依据
type ConfigVersion struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TargetType string    `gorm:"size:16;index:idx_cfg_target" json:"targetType"` // server / node
	TargetID   uint      `gorm:"index:idx_cfg_target" json:"targetId"`
	Version    int       `json:"version"`
	Checksum   string    `gorm:"size:64" json:"checksum"`
	Content    string    `gorm:"type:text" json:"content"`
	Status     string    `gorm:"size:32;default:pending" json:"status"` // pending / applied / failed / rolled_back
	Message    string    `gorm:"size:512" json:"message"`
	CreatedAt  time.Time `json:"createdAt"`
}

// AgentCommand 面板下发给 Agent 的指令；Agent 离线时排队，上线后补发
type AgentCommand struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	AgentID    uint   `gorm:"index" json:"agentId"`
	Type       string `gorm:"size:32" json:"type"` // apply / start / stop / restart / log / rollback
	TargetType string `gorm:"size:16" json:"targetType"`
	TargetID   uint   `json:"targetId"`
	Payload    string `gorm:"type:text" json:"payload"` // apply / rollback 时为配置全文
	Version    int    `json:"version"`
	TimeoutMs  int    `json:"timeoutMs"`

	Status  string    `gorm:"size:32;default:pending" json:"status"` // pending / sent / done / failed
	Result  string    `gorm:"size:1024" json:"result"`
	SentAt  *time.Time `json:"sentAt"`
	DoneAt  *time.Time `json:"doneAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
