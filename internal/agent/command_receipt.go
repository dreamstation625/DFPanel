package agent

import (
	"dfpanel/internal/proto"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type commandReceipt struct {
	State       string           `json:"state"`
	Fingerprint string           `json:"fingerprint"`
	Result      proto.ResultData `json:"result"`
}

// executeCommand 串行执行并持久化收据。结果丢失时重发收据；崩溃中断的命令明确报错，避免盲目重复副作用。
func (a *Agent) executeCommand(cmd proto.CommandData) proto.ResultData {
	a.execMu.Lock()
	defer a.execMu.Unlock()
	if cmd.CommandID == 0 {
		return proto.ResultData{Message: "缺少命令编号"}
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return proto.ResultData{Message: err.Error()}
	}
	fingerprint := checksumOf(string(data))
	path := filepath.Join(a.cfg.DataDir, "command-receipts", checksumOf(a.cfg.NodeKey), fmt.Sprintf("%d.json", cmd.CommandID))
	if raw, err := os.ReadFile(path); err == nil {
		var saved commandReceipt
		if err := json.Unmarshal(raw, &saved); err != nil {
			return proto.ResultData{Message: "命令收据损坏，需要人工核实"}
		}
		if saved.Fingerprint != fingerprint {
			return proto.ResultData{Message: "同一命令编号的内容不一致"}
		}
		if saved.State == "done" {
			return saved.Result
		}
		return proto.ResultData{Message: "Agent 在执行中重启，结果无法确认；请检查实例状态后重新操作"}
	} else if !os.IsNotExist(err) {
		return proto.ResultData{Message: "无法读取命令收据：" + err.Error()}
	}
	receipt := commandReceipt{State: "running", Fingerprint: fingerprint}
	if err := saveReceipt(path, receipt); err != nil {
		return proto.ResultData{Message: "保存命令收据失败：" + err.Error()}
	}
	rd := a.handleCommand(cmd)
	receipt.State = "done"
	receipt.Result = rd
	if err := saveReceipt(path, receipt); err != nil {
		return proto.ResultData{Message: "命令已执行，但保存结果失败；请核实运行状态：" + err.Error()}
	}
	return rd
}

func saveReceipt(path string, r commandReceipt) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, string(b))
}
