package model

import "time"

// AuthSession 保存可撤销的登录会话；旧版无会话编号的令牌不再接受。
type AuthSession struct {
	ID        string    `gorm:"primaryKey;size:64"`
	UserID    uint      `gorm:"index;not null"`
	ExpiresAt time.Time `gorm:"index"`
}

// AgentNonce 将重放保护持久化，使面板重启后旧签名仍然不可重用。
type AgentNonce struct {
	ID        string    `gorm:"primaryKey;size:160"`
	ExpiresAt time.Time `gorm:"index"`
}
