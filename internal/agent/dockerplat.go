package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// InContainer 判断 Agent 自己是不是跑在容器里。
//
// 只用于给 runtime=docker 的挂载路径把关：Agent 在容器里时，它看到的路径
// （/var/lib/dfpanel-agent/...）宿主 docker daemon 看不到，直接拿来 bind-mount
// 会被 docker 建出一个同名空目录，报出来的却是「exec: is a directory」。
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		for _, marker := range []string{"docker", "containerd", "kubepods", "podman", "lxc"} {
			if strings.Contains(s, marker) {
				return true
			}
		}
	}
	return false
}

// 容器平台探测结果缓存：docker info 有几十到几百毫秒开销，
// 而版本状态查询比较频繁，故缓存一段时间。
const containerPlatformTTL = 5 * time.Minute

var (
	containerPlatMu   sync.Mutex
	containerPlatOS   string
	containerPlatArch string
	containerPlatAt   time.Time
	containerPlatErr  string
)

// containerPlatform 探测 docker 实际运行的容器平台（不是 Agent 宿主的平台）。
//
// 这点很关键：Windows / macOS 上 Docker Desktop 跑的是 Linux 容器，
// 此时必须下载 linux/amd64 的 frp 二进制，而不是宿主机的 windows/amd64。
func containerPlatform() (string, string, error) {
	containerPlatMu.Lock()
	if time.Since(containerPlatAt) < containerPlatformTTL {
		osName, arch, errMsg := containerPlatOS, containerPlatArch, containerPlatErr
		containerPlatMu.Unlock()
		if errMsg != "" {
			return "", "", fmt.Errorf("%s", errMsg)
		}
		return osName, arch, nil
	}
	containerPlatMu.Unlock()

	osName, arch, err := probeContainerPlatform()

	containerPlatMu.Lock()
	containerPlatOS, containerPlatArch = osName, arch
	containerPlatAt = time.Now()
	if err != nil {
		containerPlatErr = err.Error()
	} else {
		containerPlatErr = ""
	}
	containerPlatMu.Unlock()

	return osName, arch, err
}

func probeContainerPlatform() (string, string, error) {
	out, err := exec.Command("docker", "info", "--format", "{{.OSType}}/{{.Architecture}}").Output()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", "", fmt.Errorf("无法探测 docker 容器平台（docker info 失败）：%s", msg)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("docker info 返回的平台格式无法识别：%q", strings.TrimSpace(string(out)))
	}

	osName := normalizeContainerOS(parts[0])
	arch := normalizeContainerArch(parts[1])
	if osName == "" || arch == "" {
		return "", "", fmt.Errorf("暂不支持的容器平台：%s/%s", parts[0], parts[1])
	}
	if osName != "linux" {
		// 版本替换依赖 bind-mount 一个可执行的 frp 二进制进容器，
		// 目前只对 Linux 容器验证成立；Windows 容器请改用 process 运行时。
		return "", "", fmt.Errorf("暂不支持在 %s 容器中替换 frp 版本，请改用 process 运行时或 Linux 容器", osName)
	}
	return osName, arch, nil
}

// normalizeContainerOS 把 docker 的 OSType 映射成 GOOS
func normalizeContainerOS(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "linux":
		return "linux"
	case "windows":
		return "windows"
	case "darwin", "macos":
		return "darwin"
	default:
		return ""
	}
}

// normalizeContainerArch 把 docker 的 Architecture 映射成 GOARCH
func normalizeContainerArch(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64", "arm64/v8":
		return "arm64"
	case "armv7l", "arm":
		return "arm"
	default:
		return ""
	}
}
