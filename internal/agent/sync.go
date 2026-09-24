package agent

import (
	"fmt"
	"log"
	"os"
	"strings"

	"dfpanel/internal/proto"
)

// syncMissingConfigs 只补齐缺失的 frp 配置；已有本地配置不会被面板数据覆盖。
// 面板只返回当前 Agent 托管对象最近一次成功应用的快照，不会把未应用的草稿提前生效。
func (a *Agent) syncMissingConfigs() error {
	configs, err := a.client.ManagedConfigs()
	if err != nil {
		return err
	}
	var failures []string
	for _, item := range configs {
		if item.TargetID == 0 || item.Version <= 0 || item.Content == "" ||
			(item.TargetType != proto.TargetNode && item.TargetType != proto.TargetServer) {
			failures = append(failures, fmt.Sprintf("无效的托管配置 %s-%d", item.TargetType, item.TargetID))
			continue
		}
		t := Target{Type: item.TargetType, ID: item.TargetID}
		kind := kindOf(t)
		if !a.cfg.HasRole(kind) {
			continue
		}
		path := a.cfg.ConfigPath(t)
		if fi, statErr := os.Stat(path); statErr == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			continue
		} else if statErr != nil && !os.IsNotExist(statErr) {
			failures = append(failures, fmt.Sprintf("检查 %s 失败：%v", path, statErr))
			continue
		}
		// 状态先落盘，确保恢复配置后即使 Agent 中途退出，也不会绕过自动启动/手动停止开关。
		if err := a.updateState(targetKey(t), func(s *instanceState) {
			s.AutoStart = item.AutoStart
			s.ManualStopped = item.ManualStopped
		}); err != nil {
			failures = append(failures, fmt.Sprintf("保存 %s-%d 启动状态失败：%v", kind, t.ID, err))
			continue
		}
		if err := writeFileAtomic(path, item.Content); err != nil {
			failures = append(failures, fmt.Sprintf("恢复 %s-%d 配置失败：%v", kind, t.ID, err))
			continue
		}
		a.saveHistory(t, item.Version, item.Content)
		a.setCurrent(t, item.Version, checksumOf(item.Content))
		log.Printf("已从面板恢复 %s-%d 的配置 v%d", kind, t.ID, item.Version)
	}
	if len(failures) > 0 {
		return fmt.Errorf("恢复托管配置时出错：%s", strings.Join(failures, "；"))
	}
	return nil
}
