package hostid

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"strings"
)

// Current 返回同一宿主机上的 Agent 共享的标识。容器可挂载宿主机 machine-id，
// 跨运行环境无法自动识别时可用 DFPANEL_HOST_ID 显式指定同一值。
func Current() string {
	if value := strings.TrimSpace(os.Getenv("DFPANEL_HOST_ID")); value != "" {
		return digest(value)
	}
	if runtime.GOOS == "linux" {
		for _, path := range []string{"/host/etc/machine-id", "/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if raw, err := os.ReadFile(path); err == nil {
				if value := strings.TrimSpace(string(raw)); value != "" {
					return digest("linux:" + value)
				}
			}
		}
	}
	hostname, _ := os.Hostname()
	return digest(runtime.GOOS + ":" + strings.ToLower(hostname))
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}
