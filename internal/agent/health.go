package agent

import (
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"
)

// 健康检查默认参数：给 frp 留出登录/监听的时间窗口
const (
	defaultHealthTimeout = 25 * time.Second
	healthProbeInterval  = 2 * time.Second
	healthMinWait        = 3 * time.Second
	dialTimeout          = 3 * time.Second
	healthLogScanBytes   = 32 * 1024
)

// Verdict 健康判定结论
type Verdict int

const (
	// VerdictOK 已确认可用
	VerdictOK Verdict = iota
	// VerdictFail 明确的 frp 报错，需要回滚
	VerdictFail
	// VerdictUnknown 进程在跑但未能确认连通：视为「不确定」，保留新配置、不回滚
	VerdictUnknown
)

// healthResult 健康检查结果
type healthResult struct {
	Verdict Verdict
	Reason  string
}

// failKeywords 只有这些「明确的 frp 报错」才会触发回滚。
// 刻意不包含 i/o timeout 之类的临时网络抖动，避免误回滚。
var failKeywords = map[string][]string{
	"frps": {
		"address already in use",
		"bind: permission denied",
		"invalid configuration",
		"failed to start",
		"parse config error",
	},
	"frpc": {
		"login to server failed",
		"connect to server error",
		"token in login doesn't match",
		"authentication failed",
		"invalid configuration",
		"connection refused",
		"no route to host",
		"parse config error",
	},
}

// successKeywords 出现即判定为启动成功
var successKeywords = map[string][]string{
	"frps": {"frps started successfully", "service started", "frps success"},
	"frpc": {"login to server success", "start proxy success"},
}

// waitHealthy 应用新配置后的可用性确认，三态结论：
//
//	VerdictOK      —— 日志出现成功关键字且连通性正常，或稳定超过观察窗口且探测通过
//	VerdictFail    —— 日志命中明确的 frp 报错，或进程直接退出（服务不可用）
//	VerdictUnknown —— 进程仍在运行但未确认连通，保留新配置，交由面板提示人工确认
func waitHealthy(ctrl *Controller, kind, cfgPath string, baseOffset int64, timeout time.Duration) healthResult {
	if timeout <= 0 {
		timeout = defaultHealthTimeout
	}
	deadline := time.Now().Add(timeout)
	start := time.Now()

	for {
		// 本次启动新增的日志
		logs := readFrom(ctrl, cfgPath, baseOffset)

		// 1. 明确的 frp 报错：立即判定失败
		if reason, bad := matchKeywords(logs, failKeywords[kind]); bad {
			return healthResult{Verdict: VerdictFail, Reason: trimReason(kind + " 报错：" + reason)}
		}

		// 2. 进程退出：服务已不可用，需回滚到上一版
		if !ctrl.Running() {
			all, _ := ctrl.Logs(healthLogScanBytes)
			return healthResult{Verdict: VerdictFail, Reason: trimReason(kind + " 进程已退出：" + lastLines(all, 5))}
		}

		// 3. 出现成功关键字且连通性正常
		if _, good := matchKeywords(logs, successKeywords[kind]); good {
			if err := probeConnect(kind, cfgPath); err == nil {
				return healthResult{Verdict: VerdictOK}
			}
		}

		// 4. 稳定超过最小观察窗口且连通性正常（不依赖日志文案）
		if time.Since(start) >= healthMinWait {
			if err := probeConnect(kind, cfgPath); err == nil {
				return healthResult{Verdict: VerdictOK}
			}
		}

		// 5. 超时：进程还在跑、日志也没有明确报错 —— 不下失败结论，保留新配置
		if time.Now().After(deadline) {
			return healthResult{
				Verdict: VerdictUnknown,
				Reason:  trimReason("未能在 " + timeout.String() + " 内确认连通（进程仍在运行且日志无明确报错），已保留新配置"),
			}
		}
		time.Sleep(healthProbeInterval)
	}
}

// probeConnect 连通性探测：frps 探测自身监听端口，frpc 探测能否连上服务端
func probeConnect(kind, cfgPath string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	addr := ""
	port := 0
	if kind == "frps" {
		if v, ok := m["bindAddr"].(string); ok {
			addr = v
		}
		if addr == "" || addr == "0.0.0.0" || addr == "::" {
			addr = "127.0.0.1"
		}
		if v, ok := m["bindPort"].(float64); ok {
			port = int(v)
		}
	} else {
		if v, ok := m["serverAddr"].(string); ok {
			addr = v
		}
		if v, ok := m["serverPort"].(float64); ok {
			port = int(v)
		}
	}
	if addr == "" || port == 0 {
		return errNoEndpoint
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(addr, itoa(port)), dialTimeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// readFrom 读取本次启动后新增的日志（进程模式按偏移量，容器模式取全量 tail）
func readFrom(ctrl *Controller, cfgPath string, baseOffset int64) string {
	if ctrl.spec.Runtime == "docker" {
		logs, _ := ctrl.Logs(healthLogScanBytes)
		return logs
	}
	path := ctrl.spec.LogPath
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	size := fi.Size()
	if size <= baseOffset {
		return ""
	}
	if size-baseOffset > healthLogScanBytes {
		baseOffset = size - healthLogScanBytes
	}
	buf := make([]byte, size-baseOffset)
	if _, err := f.ReadAt(buf, baseOffset); err != nil {
		return ""
	}
	return string(buf)
}

func matchKeywords(logs string, keywords []string) (string, bool) {
	if logs == "" {
		return "", false
	}
	lower := strings.ToLower(logs)
	for _, k := range keywords {
		if strings.Contains(lower, strings.ToLower(k)) {
			return k, true
		}
	}
	return "", false
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func trimReason(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
