package handler

import (
	"log"
	"os"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
)

// autoStartFlags 把实例的自动启动开关打包成指令 flags，随 apply 一起下发给 Agent。
// Agent 收到后写进本地状态文件，重启时据此决定要不要把实例拉起来。
func autoStartFlags(autoStart bool) string {
	if autoStart {
		return `{"autoStart":true}`
	}
	return `{"autoStart":false}`
}

// markServerStopped 记录服务端的手动启停意图：
// 手动停过的实例在面板 / Agent 重启后不再自动拉起，手动启动或重启后恢复。
func markServerStopped(id uint, stopped bool) error {
	return database.DB.Model(&model.FrpsServer{}).Where("id = ?", id).
		Update("manual_stopped", stopped).Error
}

// StartAutoServers 面板启动时把「开了自动启动、且没被手动停过」的本机服务端拉起来。
//
// 交给 Agent 托管的实例不在这里处理 —— Agent 侧按自己的本地状态恢复。
// 使用最近可恢复快照；升级前已有磁盘配置时先留存，避免把未应用的表单改动直接启动。
func (h *ServerHandler) StartAutoServers() {
	mgr := h.mgr
	if !mgr.IsInstalled() {
		return
	}
	var servers []model.FrpsServer
	database.DB.Where("auto_start = ?", true).Find(&servers)
	for _, s := range servers {
		h.startAutoServer(s.ID)
	}
}
func (h *ServerHandler) startAutoServer(id uint) {
	unlock := lockTarget("server", id)
	defer unlock()
	var s model.FrpsServer
	if err := database.DB.First(&s, id).Error; err != nil {
		return
	}
	if !s.AutoStart || s.ManualStopped || isAgentMode(&s) || h.mgr.Running(id) {
		return
	}
	var snapshot model.ConfigVersion
	if err := database.DB.Where("target_type = ? AND target_id = ? AND status IN ?", "server", id, []string{"applied", "unverified"}).Order("version desc").First(&snapshot).Error; err != nil {
		// 旧版本没有本机快照：仅迁移磁盘上已存在的配置，不把未应用的表单当成运行配置。
		content, err := os.ReadFile(h.mgr.ConfigPath(id))
		if err != nil {
			log.Printf("服务端「%s」没有可恢复的应用配置，请先应用配置", s.Name)
			return
		}
		cv, err := recordConfigVersion("server", id, string(content))
		if err != nil {
			log.Printf("保存旧配置快照失败：%v", err)
			return
		}
		snapshot = *cv
		setConfigVersionStatus(cv.ID, "unverified", "升级时保存本机已有配置，尚未确认运行")
	}
	result := h.mgr.Apply(id, snapshot.Content, "start")
	if !result.OK {
		log.Printf("自动启动服务端「%s」失败：%s", s.Name, result.Message)
		return
	}
	setConfigVersionStatus(snapshot.ID, "applied", result.Message)
	log.Printf("已自动启动服务端「%s」（frps-%d）", s.Name, id)
}
