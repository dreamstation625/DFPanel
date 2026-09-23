//go:build windows

package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Windows 下 os.FindProcess 对任意 pid 都返回成功，判断存活必须直接问系统。
// 这里用 kernel32 的调用，不额外引入 golang.org/x/sys。
const (
	processQueryLimitedInformation = 0x1000
	processTerminate               = 0x0001
	stillActive                    = 259
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess         = kernel32.NewProc("GetExitCodeProcess")
	procTerminateProcess           = kernel32.NewProc("TerminateProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

func openProcess(access uint32, pid int) uintptr {
	if pid <= 0 {
		return 0
	}
	h, _, _ := procOpenProcess.Call(uintptr(access), 0, uintptr(uint32(pid)))
	return h
}

// processAlive 进程是否还在（是否仍是活动进程）
func processAlive(pid int) bool {
	h := openProcess(processQueryLimitedInformation, pid)
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)

	var code uint32
	if r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code))); r == 0 {
		return false
	}
	return code == stillActive
}

// processMatchesBinary 比对进程映像路径，避免 pid 被复用后误接管别人的进程。
// 拿不到路径时返回 false —— 宁可重新启动（端口冲突会明确报错），也不要错认。
func processMatchesBinary(pid int, binPath, _ string) bool {
	if binPath == "" {
		return false
	}
	h := openProcess(processQueryLimitedInformation, pid)
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)

	buf := make([]uint16, syscall.MAX_PATH)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return false
	}
	image := syscall.UTF16ToString(buf[:size])
	return strings.EqualFold(filepath.Clean(image), filepath.Clean(binPath))
}

// killProcess 强杀进程：接管的实例没有 cmd 句柄，只能按 pid 杀
func killProcess(pid int) error {
	h := openProcess(processTerminate|processQueryLimitedInformation, pid)
	if h == 0 {
		return fmt.Errorf("打开进程 %d 失败（可能已退出）", pid)
	}
	defer procCloseHandle.Call(h)

	if r, _, err := procTerminateProcess.Call(h, 1); r == 0 {
		return fmt.Errorf("结束进程 %d 失败：%v", pid, err)
	}
	return nil
}
