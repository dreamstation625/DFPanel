package agent

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// instanceState 单个实例的自动启动意图。
//
// 只存在 Agent 本地：面板下发配置时把 autoStart 一起带过来（指令 flags），
// 手动启停会翻转 manualStopped；Agent 重启后按这份状态决定恢复哪些实例。
type instanceState struct {
	AutoStart     bool `json:"autoStart"`
	ManualStopped bool `json:"manualStopped"`
}

// stateMu 保护状态文件读写（与 a.mu 分开，避免和控制器锁互相等待）
var stateMu sync.Mutex

// statePath 实例状态文件路径
func (a *Agent) statePath() string {
	return filepath.Join(a.cfg.DataDir, "instance-state.json")
}

// loadStates 读取全部实例状态；文件不存在或损坏时返回空表
func (a *Agent) loadStates() map[string]instanceState {
	out := map[string]instanceState{}
	b, err := os.ReadFile(a.statePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// trackedState 读取某个实例的状态，第二个返回值表示有没有记录。
// 没有记录的是旧版本留下的实例，恢复时沿用原来的自愈行为。
func (a *Agent) trackedState(key string) (instanceState, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	st, ok := a.loadStates()[key]
	return st, ok
}

// updateState 更新某个实例的状态并落盘
func (a *Agent) updateState(key string, fn func(*instanceState)) {
	stateMu.Lock()
	defer stateMu.Unlock()
	states := a.loadStates()
	st := states[key]
	fn(&st)
	states[key] = st
	b, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(a.statePath(), b, 0o600); err != nil {
		log.Printf("写入实例状态失败：%v", err)
	}
}

// applyAutoStartFlag 解析 apply 指令里带的自动启动开关
func (a *Agent) applyAutoStartFlag(t Target, flags string) {
	if flags == "" {
		return
	}
	var f struct {
		AutoStart *bool `json:"autoStart"`
	}
	if err := json.Unmarshal([]byte(flags), &f); err != nil || f.AutoStart == nil {
		return
	}
	auto := *f.AutoStart
	a.updateState(targetKey(t), func(s *instanceState) { s.AutoStart = auto })
}
