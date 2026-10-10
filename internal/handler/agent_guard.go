package handler

import (
	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"time"
)

// agentIdentityMu 使鉴权、绑定及凭据撤销形成同一临界区；WS 只锁到接入，不锁整个连接。
var agentIdentityMu = &agenthub.IdentityMu

func consumeNonce(key string) bool {
	now := time.Now()
	if err := database.DB.Where("expires_at < ?", now).Delete(&model.AgentNonce{}).Error; err != nil {
		return false
	}
	var count int64
	if err := database.DB.Model(&model.AgentNonce{}).Count(&count).Error; err != nil || count >= 100000 {
		return false
	}
	return database.DB.Create(&model.AgentNonce{ID: key, ExpiresAt: now.Add(10 * time.Minute)}).Error == nil
}
