package frp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ApplyResult struct {
	OK         bool
	RolledBack bool
	Running    bool
	Message    string
}

// Apply 本机应用、启动及重启共用验证和恢复流程。mode=apply 时保留原来的启停状态。
func (m *Manager) Apply(id uint, content, mode string) ApplyResult {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	wasRunning := m.Running(id)
	if mode == "start" && wasRunning {
		return ApplyResult{Message: "frps 已在运行", Running: true}
	}
	path := m.ConfigPath(id)
	old, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return ApplyResult{Message: "读取原配置失败：" + readErr.Error(), Running: wasRunning}
	}
	tmp, err := os.CreateTemp(m.dataDir, ".frps-verify-*.json")
	if err != nil {
		return ApplyResult{Message: err.Error(), Running: wasRunning}
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.WriteString(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return ApplyResult{Message: "写入候选配置失败：" + err.Error(), Running: wasRunning}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.BinPath(), "verify", "-c", tmpPath).CombinedOutput()
	if err != nil {
		return ApplyResult{Message: "配置校验失败，已保持原配置：" + strings.TrimSpace(string(out)) + " " + err.Error(), Running: wasRunning}
	}
	if readErr == nil {
		if err := atomicConfig(path+".bak", string(old)); err != nil {
			return ApplyResult{Message: "保存原配置失败：" + err.Error(), Running: wasRunning}
		}
	}
	if err := atomicConfig(path, content); err != nil {
		return ApplyResult{Message: "写入配置失败：" + err.Error(), Running: wasRunning}
	}
	if mode == "apply" && !wasRunning {
		return ApplyResult{OK: true, Message: "配置已保存并通过校验，实例保持停止"}
	}
	err = m.stop(id)
	if err == nil {
		err = m.start(id)
	}
	if err == nil {
		err = m.waitHealthy(id, content)
	}
	if err == nil {
		return ApplyResult{OK: true, Running: true, Message: "配置已生效，frps 已通过健康检查"}
	}
	reason := err.Error()
	if stopErr := m.stop(id); stopErr != nil {
		return ApplyResult{Message: reason + "；停止失败，未恢复配置：" + stopErr.Error(), Running: m.Running(id)}
	}
	if readErr == nil {
		err = atomicConfig(path, string(old))
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return ApplyResult{Message: reason + "；还原配置失败：" + err.Error()}
	}
	r := ApplyResult{RolledBack: readErr == nil, Message: reason + "；已恢复原配置和启停状态"}
	if wasRunning {
		err = m.start(id)
		if err == nil {
			err = m.waitHealthy(id, string(old))
		}
		if err != nil {
			r.Message = reason + "；原配置已还原，但恢复服务失败：" + err.Error()
		}
	}
	r.Running = m.Running(id)
	return r
}

func atomicConfig(path, content string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.WriteString(content)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *Manager) waitHealthy(id uint, content string) error {
	var cfg struct {
		BindAddr string `json:"bindAddr"`
		BindPort int    `json:"bindPort"`
	}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return err
	}
	if cfg.BindPort == 0 {
		cfg.BindPort = 7000
	}
	host := cfg.BindAddr
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(cfg.BindPort))
	start := time.Now()
	deadline := start.Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !m.Running(id) {
			return fmt.Errorf("frps 启动后退出")
		}
		conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			if time.Since(start) > 1500*time.Millisecond {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("frps 未在观察窗口内监听 %s", addr)
}
