package handler

import (
	"log"

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
func markServerStopped(id uint, stopped bool) {
	_ = database.DB.Model(&model.FrpsServer{}).Where("id = ?", id).
		Update("manual_stopped", stopped).Error
}

// markNodeStopped 同上，节点（frpc）版本
func markNodeStopped(id uint, stopped bool) {
	_ = database.DB.Model(&model.Node{}).Where("id = ?", id).
		Update("manual_stopped", stopped).Error
}

// StartAutoServers 面板启动时把「开了自动启动、且没被手动停过」的本机服务端拉起来。
//
// 交给 Agent 托管的实例不在这里处理 —— Agent 侧按自己的本地状态恢复。
// 配置文件和配置一样由面板生成，所以这里要先写一遍配置再启动（面板重启后文件可能还没落盘）。
func (h *ServerHandler) StartAutoServers() {
	mgr := h.mgr
	if !mgr.IsInstalled() {
		return
	}
	var servers []model.FrpsServer
	database.DB.Where("auto_start = ?", true).Find(&servers)
	for _, s := range servers {
		if isAgentMode(&s) || mgr.Running(s.ID) {
			continue
		}
		if s.ManualStopped {
			log.Printf("服务端「%s」被手动停过，跳过自动启动", s.Name)
			continue
		}
		content, err := h.buildConfig(&s)
		if err != nil {
			log.Printf("自动启动服务端「%s」失败：生成配置出错：%v", s.Name, err)
			continue
		}
		if err := mgr.WriteConfig(s.ID, content); err != nil {
			log.Printf("自动启动服务端「%s」失败：写入配置出错：%v", s.Name, err)
			continue
		}
		if err := mgr.Start(s.ID); err != nil {
			log.Printf("自动启动服务端「%s」失败：%v", s.Name, err)
			continue
		}
		log.Printf("已自动启动服务端「%s」（frps-%d）", s.Name, s.ID)
	}
}
