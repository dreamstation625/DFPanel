package model

import "time"

// AgentFRPCachedVersionsColumn 固定现有数据库列名，避免改名时丢失已缓存版本记录。
// GORM 将 FRPCachedVersions 默认映射为 f_rpcached_versions。
const AgentFRPCachedVersionsColumn = "f_rpcached_versions"

// Agent 部署在远端机器上的守护程序（单一角色、单一托管对象）
type Agent struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	Name   string `gorm:"size:64;not null" json:"name"`
	Remark string `gorm:"size:255" json:"remark"`

	// NodeKey 安装令牌（公开），Secret 签名密钥（不出网，仅安装命令中出现一次）
	NodeKey string `gorm:"uniqueIndex;size:64;not null" json:"nodeKey"`
	Secret  string `gorm:"size:128;not null" json:"secret"`

	// Roles 只能是 frps 或 frpc；一条 Agent 记录只代表一个安装实例。
	Roles      string `gorm:"size:64;default:frpc" json:"roles"`
	InstanceID string `gorm:"size:64;uniqueIndex:idx_agent_instance,where:instance_id <> ''" json:"-"`
	HostID     string `gorm:"size:128;index" json:"hostId"`
	// Runtime 运行时：process（直起子进程）/ docker（起容器）；由 Agent 上报
	Runtime string `gorm:"size:16" json:"runtime"`

	// 以下三个字段是 frp（frps/frpc）的版本信息，与该 Agent 自身的 Version 不是一回事。
	// 版本粒度按 Agent 记录独立管理。
	// FRPVersion 为空表示不管理，此时沿用机器上现有的二进制，仅提示可更新。
	FRPVersion          string `gorm:"size:32" json:"frpVersion"`
	FRPInstalledVersion string `gorm:"size:32" json:"frpInstalledVersion"`                           // active 槽位实际版本（心跳上报）
	FRPCachedVersions   string `gorm:"column:f_rpcached_versions;size:512" json:"frpCachedVersions"` // 已缓存版本，逗号分隔（心跳上报）
	// FRPUpdatedAt 最近一次版本切换时间
	FRPUpdatedAt *time.Time `json:"frpUpdatedAt"`

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
	return a.Roles == role
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
	Type       string `gorm:"size:32" json:"type"` // apply / start / stop / restart / unassign / log / rollback
	TargetType string `gorm:"size:16" json:"targetType"`
	TargetID   uint   `json:"targetId"`
	Payload    string `gorm:"type:text" json:"payload"` // apply / rollback 时为配置全文
	Version    int    `json:"version"`
	Flags      string `gorm:"size:256" json:"flags"` // 附带元信息，例如 apply 时的自动启动开关
	TimeoutMs  int    `json:"timeoutMs"`

	Status    string     `gorm:"size:32;default:pending" json:"status"` // pending / sent / done / failed
	Result    string     `gorm:"size:1024" json:"result"`
	SentAt    *time.Time `json:"sentAt"`
	DoneAt    *time.Time `json:"doneAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}
