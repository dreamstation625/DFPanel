package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"sync"
	"time"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

// 默认等待 Agent 执行结果的时长（含回滚探测时间）
const defaultDispatchTimeout = 90 * time.Second

// dispatchResult 指令下发结果
type dispatchResult struct {
	OK         bool
	Queued     bool // Agent 离线，指令已排队
	Message    string
	Content    string
	Running    bool
	Version    int
	Checksum   string
	RolledBack bool
	Unverified bool // 已应用但未在观察窗口内确认连通
	Versions   []proto.HistoryEntry
	// FrpVersion / FrpCached frp 版本指令的返回：当前 active 版本与本地已缓存版本
	FrpVersion string
	FrpCached  []string
}

// dispatch 把指令下发给指定 Agent：
// 在线时走 WebSocket 同步等待结果；离线时写入指令队列，等待 Agent 上线后轮询补发。
func dispatch(hub *agenthub.Hub, cmd *model.AgentCommand) (dispatchResult, error) {
	if cmd.AgentID == 0 {
		return dispatchResult{}, errors.New("未指定托管 Agent")
	}
	if cmd.TimeoutMs <= 0 {
		cmd.TimeoutMs = int(defaultDispatchTimeout.Milliseconds())
	}
	cmd.DeliveryVersion = 2
	if err := database.DB.Create(cmd).Error; err != nil {
		return dispatchResult{}, fmt.Errorf("创建指令失败：%w", err)
	}

	if !hub.Online(cmd.AgentID) {
		return dispatchResult{Queued: true, Message: "Agent 离线，指令已排队，上线后自动执行"}, nil
	}
	claimed, err := claimCommand(cmd.AgentID, cmd.ID)
	if err != nil {
		return dispatchResult{}, err
	}
	if claimed == nil {
		return dispatchResult{Queued: true, Message: "前序指令仍在处理，本指令已排队"}, nil
	}
	data, err := json.Marshal(proto.CommandData{
		CommandID:  cmd.ID,
		Type:       cmd.Type,
		TargetType: cmd.TargetType,
		TargetID:   cmd.TargetID,
		Payload:    cmd.Payload,
		Version:    cmd.Version,
		Flags:      cmd.Flags,
	})
	if err != nil {
		return dispatchResult{}, err
	}

	env := proto.Envelope{
		Type: proto.MsgCommand,
		ID:   fmt.Sprintf("cmd-%d", cmd.ID),
		Data: data,
	}

	resp, err := hub.Send(cmd.AgentID, env, time.Duration(cmd.TimeoutMs)*time.Millisecond)
	if errors.Is(err, agenthub.ErrOffline) {
		return dispatchResult{Queued: true, Message: "Agent 离线，指令已排队，上线后自动执行"}, nil
	}
	if err != nil {
		return dispatchResult{Queued: true, Message: "暂未收到执行结果，指令保留并自动重试"}, nil
	}

	var rd proto.ResultData
	if err := json.Unmarshal(resp.Data, &rd); err != nil {
		return dispatchResult{}, fmt.Errorf("解析 Agent 响应失败：%w", err)
	}

	if err := finishCommand(hub, cmd, rd); err != nil {
		return dispatchResult{}, err
	}
	msg := rd.Message

	return dispatchResult{
		OK:         rd.OK,
		Message:    msg,
		Content:    rd.Content,
		Running:    rd.Running,
		Version:    rd.Version,
		Checksum:   rd.Checksum,
		RolledBack: rd.RolledBack,
		Unverified: rd.Unverified,
		Versions:   rd.Versions,
		FrpVersion: rd.FrpVersion,
		FrpCached:  rd.FrpCached,
	}, nil
}

var versionMu sync.Mutex

// recordConfigVersion 在事务内分配版本并保存快照；失败时禁止继续下发。
func recordConfigVersion(targetType string, targetID uint, content string) (*model.ConfigVersion, error) {
	versionMu.Lock()
	defer versionMu.Unlock()
	cv := &model.ConfigVersion{TargetType: targetType, TargetID: targetID, Content: content, Checksum: checksum(content), Status: "pending", CreatedAt: time.Now()}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var last int
		if err := tx.Model(&model.ConfigVersion{}).Where("target_type = ? AND target_id = ?", targetType, targetID).Select("COALESCE(MAX(version),0)").Scan(&last).Error; err != nil {
			return err
		}
		cv.Version = last + 1
		return tx.Create(cv).Error
	})
	return cv, err
}

// setConfigVersionStatus 更新配置快照的应用状态
func setConfigVersionStatus(id uint, status, message string) {
	_ = database.DB.Model(&model.ConfigVersion{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "message": message}).Error
}

func checksum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
