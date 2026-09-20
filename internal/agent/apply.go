package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dfpanel/internal/proto"
)

// verifyTimeout frp verify 子命令的超时时间
const verifyTimeout = 10 * time.Second

// errNoEndpoint 配置中缺少可探测的地址端口
var errNoEndpoint = errors.New("配置中缺少可探测的地址端口")

// ApplyOutcome 配置应用结果
type ApplyOutcome struct {
	OK         bool
	RolledBack bool
	// Unverified 已应用并重启，但未在观察窗口内确认连通（不回滚，提示人工确认）
	Unverified bool
	Message    string
	Version    int
	Checksum   string
}

// ApplyConfig 应用新配置并做可用性确认：
//
//  1. 备份当前配置（上一份可用配置）
//  2. 原子写入新配置
//  3. frp verify 语法校验（失败则直接还原，不动正在运行的进程）
//  4. 重启并做健康检查：进程存活 + 能连上服务端 + 日志无致命错误
//  5. 健康检查失败则自动回滚到上一份配置并重新拉起
func (a *Agent) ApplyConfig(t Target, content string, version int) ApplyOutcome {
	kind := kindOf(t)
	if !a.cfg.HasRole(kind) {
		return ApplyOutcome{Message: fmt.Sprintf("Agent 未启用 %s 角色，请在面板为 Agent 分配角色", kind)}
	}

	cfgPath := a.cfg.ConfigPath(t)
	bakPath := a.cfg.BackupPath(t)

	// 进程运行时需要本地二进制，缺失时从面板拉取
	if a.cfg.Runtime == "process" {
		if _, err := os.Stat(a.cfg.BinaryPath(kind)); err != nil {
			if _, err := a.client.DownloadBinary(kind); err != nil {
				return ApplyOutcome{Message: fmt.Sprintf("获取 %s 二进制失败：%v", kind, err)}
			}
		}
	}

	// 备份当前（可用）配置，作为回滚依据
	hasBackup := false
	if data, err := os.ReadFile(cfgPath); err == nil && len(data) > 0 {
		if err := writeFileAtomic(bakPath, string(data)); err == nil {
			hasBackup = true
		}
	}

	if err := writeFileAtomic(cfgPath, content); err != nil {
		return ApplyOutcome{Message: "写入配置失败：" + err.Error()}
	}
	// 每次收到面板下发的配置都留一份快照，供面板查看历史版本与按需回滚
	a.saveHistory(t, version, content)

	// 语法校验失败不重启进程，避免把本来能用的服务搞挂
	if a.cfg.Runtime == "process" {
		if err := verifyConfig(a.cfg.BinaryPath(kind), cfgPath); err != nil {
			restoreBackup(bakPath, cfgPath, hasBackup)
			return ApplyOutcome{Message: "配置校验失败，已保持原配置：" + err.Error()}
		}
	}

	ctrl := a.controller(t)
	baseOffset := ctrl.LogSize()

	wasRunning := ctrl.Running()
	_ = ctrl.Stop()
	if err := ctrl.Start(); err != nil {
		if wasRunning {
			return a.rollback(t, ctrl, "启动失败："+err.Error(), hasBackup)
		}
		restoreBackup(bakPath, cfgPath, hasBackup)
		return ApplyOutcome{Message: "启动失败：" + err.Error()}
	}

	health := waitHealthy(ctrl, kind, cfgPath, baseOffset, defaultHealthTimeout)
	switch health.Verdict {
	case VerdictOK:
		sum := checksumOf(content)
		a.setCurrent(t, version, sum)
		return ApplyOutcome{OK: true, Version: version, Checksum: sum, Message: "配置已生效"}

	case VerdictUnknown:
		// 进程在跑、日志无明确报错：保守起见保留新配置，仅提示人工确认
		sum := checksumOf(content)
		a.setCurrent(t, version, sum)
		return ApplyOutcome{
			OK:         true,
			Unverified: true,
			Version:    version,
			Checksum:   sum,
			Message:    health.Reason + "；如需回退可在面板「历史版本」中选择",
		}

	default:
		return a.rollback(t, ctrl, health.Reason, hasBackup)
	}
}

// rollback 恢复上一份可用配置并重新拉起
func (a *Agent) rollback(t Target, ctrl *Controller, reason string, hasBackup bool) ApplyOutcome {
	kind := kindOf(t)
	cfgPath := a.cfg.ConfigPath(t)
	bakPath := a.cfg.BackupPath(t)

	_ = ctrl.Stop()
	if !hasBackup {
		return ApplyOutcome{Message: reason + "；无可用历史配置，未回滚"}
	}

	restoreBackup(bakPath, cfgPath, true)
	baseOffset := ctrl.LogSize()
	if err := ctrl.Start(); err != nil {
		return ApplyOutcome{RolledBack: true, Message: reason + "；已还原配置但重启失败：" + err.Error()}
	}
	if h := waitHealthy(ctrl, kind, cfgPath, baseOffset, defaultHealthTimeout); h.Verdict == VerdictFail {
		return ApplyOutcome{RolledBack: true, Message: reason + "；已还原配置但服务仍未就绪：" + h.Reason}
	}
	return ApplyOutcome{RolledBack: true, Message: reason + "；已回滚至上一版配置并恢复运行"}
}

// writeFileAtomic 先写临时文件再重命名，避免半截配置被进程读到
func writeFileAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func restoreBackup(bakPath, cfgPath string, hasBackup bool) {
	if !hasBackup {
		return
	}
	data, err := os.ReadFile(bakPath)
	if err != nil {
		return
	}
	_ = writeFileAtomic(cfgPath, string(data))
}

// verifyConfig 调用 frp 自带的 verify 子命令校验配置语法
func verifyConfig(bin, cfgPath string) error {
	if _, err := os.Stat(bin); err != nil {
		// 二进制缺失时无法校验，交由后续启动与健康检查兜底
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()

	out, err := execCommand(ctx, bin, "verify", "-c", cfgPath)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func kindOf(t Target) string {
	if t.Type == proto.TargetServer {
		return "frps"
	}
	return "frpc"
}

func checksumOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
