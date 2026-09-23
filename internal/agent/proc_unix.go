//go:build !windows

package agent

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// processAlive 进程是否还在（signal 0 只做探活，不会真的发信号）。
// EPERM 说明进程存在、只是当前用户没权限操作它 —— 同样算活着。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

// processMatchesBinary 校验 pid 对应的进程确实是我们要接管的那个实例。
//
// Linux 读 /proc/<pid>/cmdline（用「配置路径」唯一标识实例，二进制路径会随版本切换变化）；
// macOS 没有 /proc，退化为「只判存活」，靠 pid 复用概率兜底。
func processMatchesBinary(pid int, _ string, cfgPath string) bool {
	if pid <= 0 {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		// 读不到（macOS、或权限不足）：不否决，交给存活判断
		return true
	}
	if cfgPath == "" {
		return true
	}
	cmdline := strings.ReplaceAll(string(b), "\x00", " ")
	return strings.Contains(cmdline, cfgPath)
}

// killProcess 强杀进程：接管的实例没有 cmd 句柄，只能按 pid 杀
func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
