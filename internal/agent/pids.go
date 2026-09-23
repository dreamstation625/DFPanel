package agent

import (
	"os"
	"strconv"
	"strings"
)

// 这里管的是「Agent 重启后接管旧实例」所需的最小状态：一个 pid 文件。
//
// Agent 退出时不会杀自己拉起的 frps / frpc（重启时隧道不中断），所以重启后必须能
// 认出「哪些实例还活着」，接管它们，而不是再起一个 —— 那会因端口被占而失败。

// readPidFile 读 pid 文件；不存在或内容不合法时返回 0
func readPidFile(path string) int {
	if path == "" {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// writePidFile 记录子进程 pid：先写临时文件再改名，避免读到写了一半的内容
func writePidFile(path string, pid int) error {
	if path == "" || pid <= 0 {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// removePidFile 删除 pid 文件（不存在时静默返回）
func removePidFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
