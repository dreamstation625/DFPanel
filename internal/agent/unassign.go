package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// unassign 删除绑定对象时清理本地运行状态，避免 Agent 重启后再次拉起旧实例。
func (a *Agent) unassign(t Target) error {
	ctrl := a.controller(t)
	if err := ctrl.Stop(); err != nil {
		return err
	}
	for _, path := range []string{a.cfg.ConfigPath(t), a.cfg.ConfigPath(t) + ".bak", a.cfg.PidPath(t)} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s：%w", path, err)
		}
	}
	a.mu.Lock()
	delete(a.ctrls, targetKey(t))
	delete(a.targets, targetKey(t))
	delete(a.curV, targetKey(t))
	delete(a.curS, targetKey(t))
	a.mu.Unlock()
	stateMu.Lock()
	states := a.loadStates()
	delete(states, targetKey(t))
	if err := a.saveStates(states); err != nil {
		stateMu.Unlock()
		return err
	}
	stateMu.Unlock()
	if entries, err := os.ReadDir(a.historyDir()); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), historyPrefix(t)) {
				_ = os.Remove(filepath.Join(a.historyDir(), entry.Name()))
			}
		}
	}
	return nil
}
