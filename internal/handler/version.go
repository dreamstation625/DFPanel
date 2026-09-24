package handler

import (
	"encoding/json"
	"time"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

// versionView 历史版本对外结构：以 Agent 本地快照为准，叠加面板侧记录的状态
type versionView struct {
	Version   int       `json:"version"`
	Size      int64     `json:"size"`
	Time      int64     `json:"time"`
	Checksum  string    `json:"checksum"`
	Current   bool      `json:"current"` // 与当前生效配置一致
	Status    string    `json:"status"`  // applied / unverified / rolled_back / failed
	Message   string    `json:"message"` // 下发结果说明（含回滚原因）
	CreatedAt time.Time `json:"createdAt"`
	OnAgent   bool      `json:"onAgent"`   // Agent 本地是否还保留该版本快照（可回滚）
	HasConfig bool      `json:"hasConfig"` // 面板或在线 Agent 是否有可查看的配置快照
}

// loadVersionRecords 面板侧的配置版本记录（content 不回传给前端）
func loadVersionRecords(targetType string, targetID uint) []versionView {
	var list []model.ConfigVersion
	database.DB.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("version desc").Limit(200).Find(&list)

	out := make([]versionView, 0, len(list))
	for _, cv := range list {
		out = append(out, versionView{
			Version:   cv.Version,
			Size:      int64(len(cv.Content)),
			Checksum:  cv.Checksum,
			Status:    cv.Status,
			Message:   cv.Message,
			CreatedAt: cv.CreatedAt,
			HasConfig: cv.Content != "",
		})
	}
	return out
}

// mergeVersions 合并 Agent 本地快照与面板记录：Agent 有文件的版本可回滚
func mergeVersions(agentList []proto.HistoryEntry, dbList []versionView) []versionView {
	byVersion := make(map[int]versionView, len(dbList))
	for _, v := range dbList {
		byVersion[v.Version] = v
	}

	out := make([]versionView, 0, len(agentList)+len(dbList))
	seen := make(map[int]bool, len(agentList))
	for _, e := range agentList {
		v := byVersion[e.Version]
		v.Version = e.Version
		v.Size = e.Size
		v.Time = e.Time
		v.Checksum = e.Checksum
		v.Current = e.Current
		v.OnAgent = true
		v.HasConfig = true
		out = append(out, v)
		seen[e.Version] = true
	}
	for _, v := range dbList {
		if !seen[v.Version] {
			out = append(out, v)
		}
	}

	// 回滚后会出现多个内容相同的版本，这里只在最新的那个上标记「当前」，避免歧义
	maxCurrent := 0
	for _, v := range out {
		if v.Current && v.Version > maxCurrent {
			maxCurrent = v.Version
		}
	}
	for i := range out {
		if out[i].Current && out[i].Version != maxCurrent {
			out[i].Current = false
		}
	}
	return out
}

// findVersionRecord 取面板侧某个版本的完整记录（回滚时用于生成新版本快照）
func findVersionRecord(targetType string, targetID uint, version int) (*model.ConfigVersion, error) {
	var cv model.ConfigVersion
	if err := database.DB.Where("target_type = ? AND target_id = ? AND version = ?",
		targetType, targetID, version).First(&cv).Error; err != nil {
		return nil, err
	}
	return &cv, nil
}

// rollbackPayload 回滚指令内容
func rollbackPayload(targetVersion int) string {
	data, _ := json.Marshal(map[string]int{"targetVersion": targetVersion})
	return string(data)
}

// updateVersionStatus 按目标与版本号回写配置版本状态
// WebSocket 同步下发路径不走 HTTP report，需要在这里落库，否则版本状态会一直停留在 pending
func updateVersionStatus(targetType string, targetID uint, version int, status, message string) {
	if version <= 0 {
		return
	}
	var cv model.ConfigVersion
	if err := database.DB.Where("target_type = ? AND target_id = ? AND version = ?",
		targetType, targetID, version).First(&cv).Error; err != nil {
		return
	}
	setConfigVersionStatus(cv.ID, status, message)
}
