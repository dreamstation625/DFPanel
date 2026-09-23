package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	if err := database.DB.Create(cmd).Error; err != nil {
		return dispatchResult{}, fmt.Errorf("创建指令失败：%w", err)
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
		markCommand(cmd.ID, "failed", err.Error())
		return dispatchResult{}, err
	}

	var rd proto.ResultData
	if err := json.Unmarshal(resp.Data, &rd); err != nil {
		return dispatchResult{}, fmt.Errorf("解析 Agent 响应失败：%w", err)
	}

	status := "done"
	msg := rd.Message
	if !rd.OK {
		status = "failed"
	}
	markCommand(cmd.ID, status, msg)

	// WebSocket 同步下发不会走 HTTP report，这里补回配置版本状态
	if cmd.Type == proto.CmdApply || cmd.Type == proto.CmdRollback {
		vst := "applied"
		switch {
		case rd.RolledBack:
			vst = "rolled_back"
		case !rd.OK:
			vst = "failed"
		case rd.Unverified:
			vst = "unverified"
		}
		updateVersionStatus(cmd.TargetType, cmd.TargetID, cmd.Version, vst, msg)
	}

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

func markCommand(id uint, status, result string) {
	now := time.Now()
	_ = database.DB.Model(&model.AgentCommand{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "result": result, "done_at": now}).Error
}

// nextVersion 返回目标对象的下一个配置版本号
func nextVersion(targetType string, targetID uint) int {
	var last model.ConfigVersion
	if err := database.DB.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("version desc").First(&last).Error; err != nil {
		return 1
	}
	return last.Version + 1
}

// recordConfigVersion 落库一份配置快照，作为回滚依据
func recordConfigVersion(targetType string, targetID uint, version int, content string) *model.ConfigVersion {
	cv := &model.ConfigVersion{
		TargetType: targetType,
		TargetID:   targetID,
		Version:    version,
		Checksum:   checksum(content),
		Content:    content,
		Status:     "pending",
		CreatedAt:  time.Now(),
	}
	if err := database.DB.Create(cv).Error; err != nil {
		return cv
	}
	return cv
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
