package handler

import (
	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"sync"
	"time"
)

var commandMu sync.Mutex

// claimCommand 只领取该 Agent 最早的未完成命令。CAS 租约让 WS 与 HTTP 不能同时领取。
func claimCommand(agentID, expectedID uint) (*model.AgentCommand, error) {
	commandMu.Lock()
	defer commandMu.Unlock()
	var out *model.AgentCommand
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var cmd model.AgentCommand
		err := tx.Where("agent_id = ? AND status IN ?", agentID, []string{"pending", "sent"}).Order("id asc").First(&cmd).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if expectedID != 0 && cmd.ID != expectedID {
			return nil
		}
		lease := time.Duration(cmd.TimeoutMs)*time.Millisecond + time.Minute
		if lease < 2*time.Minute {
			lease = 2 * time.Minute
		}
		now := time.Now()
		if cmd.Status == "sent" && cmd.SentAt != nil && now.Sub(*cmd.SentAt) < lease {
			return nil
		}
		q := tx.Model(&model.AgentCommand{}).Where("id = ? AND status = ?", cmd.ID, cmd.Status)
		if cmd.Status == "sent" {
			if cmd.SentAt == nil {
				q = q.Where("sent_at IS NULL")
			} else {
				q = q.Where("sent_at = ?", cmd.SentAt)
			}
		}
		res := q.Updates(map[string]any{"status": "sent", "sent_at": now, "delivery_version": 2})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		cmd.Status = "sent"
		cmd.SentAt = &now
		out = &cmd
		return nil
	})
	return out, err
}

func commandData(cmd *model.AgentCommand) proto.CommandData {
	return proto.CommandData{CommandID: cmd.ID, Type: cmd.Type, TargetType: cmd.TargetType, TargetID: cmd.TargetID, Payload: cmd.Payload, Version: cmd.Version, Flags: cmd.Flags}
}

// finishCommand 在同一事务中写命令结果、配置状态及启停意图；重复报告不能覆盖终态。
func finishCommand(hub *agenthub.Hub, cmd *model.AgentCommand, rd proto.ResultData) error {
	data, err := json.Marshal(rd)
	if err != nil {
		return err
	}
	changed := false
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		status := "done"
		if !rd.OK {
			status = "failed"
		}
		res := tx.Model(&model.AgentCommand{}).Where("id = ? AND agent_id = ? AND status = ?", cmd.ID, cmd.AgentID, "sent").Updates(map[string]any{"status": status, "result": rd.Message, "result_data": string(data), "done_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		changed = true
		if cmd.Type == proto.CmdApply || cmd.Type == proto.CmdRollback {
			st := "applied"
			switch {
			case rd.RolledBack:
				st = "rolled_back"
			case !rd.OK:
				st = "failed"
			case rd.Unverified:
				st = "unverified"
			}
			if err := tx.Model(&model.ConfigVersion{}).Where("target_type = ? AND target_id = ? AND version = ?", cmd.TargetType, cmd.TargetID, cmd.Version).Updates(map[string]any{"status": st, "message": rd.Message}).Error; err != nil {
				return err
			}
		}
		if rd.OK && (cmd.Type == proto.CmdStop || cmd.Type == proto.CmdStart || cmd.Type == proto.CmdRestart) {
			var target any
			switch cmd.TargetType {
			case proto.TargetNode:
				target = &model.Node{}
			case proto.TargetServer:
				target = &model.FrpsServer{}
			}
			if target != nil {
				return tx.Model(target).Where("id = ? AND agent_id = ?", cmd.TargetID, cmd.AgentID).Update("manual_stopped", cmd.Type == proto.CmdStop).Error
			}
		}
		return nil
	})
	if err == nil && changed && hub != nil && cmd.TargetID > 0 {
		hub.SetTargetRunning(cmd.AgentID, cmd.TargetType, cmd.TargetID, rd.Running)
	}
	return err
}
