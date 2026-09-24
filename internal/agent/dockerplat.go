package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// hostMountTTL 自己这个容器的挂载映射缓存时长（中途换挂载不常见）
const hostMountTTL = 5 * time.Minute

var (
	hostMountMu  sync.Mutex
	hostMountVal map[string]string
	hostMountAt  time.Time
)

// cachedHostMounts 带缓存的「容器内路径 → 宿主路径」映射
func cachedHostMounts() map[string]string {
	hostMountMu.Lock()
	if hostMountVal != nil && time.Since(hostMountAt) < hostMountTTL {
		m := hostMountVal
		hostMountMu.Unlock()
		return m
	}
	hostMountMu.Unlock()

	m := probeHostMounts()

	hostMountMu.Lock()
	hostMountVal, hostMountAt = m, time.Now()
	hostMountMu.Unlock()
	return m
}

// probeHostMounts 通过 docker inspect 查自己这个容器的挂载映射。
//
// 为什么要反查：compose 里写相对路径（./agent-data）或命名卷时，宿主上的绝对路径
// 只有 docker daemon 知道，配置里根本写不出来 —— 而 frp 容器恰恰需要那个绝对路径。
// 容器名/hostname 在面板生成的安装命令里都是 dfpanel-agent，inspect 得到的是自己。
func probeHostMounts() map[string]string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return nil
	}
	out, err := exec.Command("docker", "inspect", "-f",
		"{{range .Mounts}}{{.Destination}}\t{{.Source}}\n{{end}}", name).Output()
	if err != nil {
		return nil
	}
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		if dst, src := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]); dst != "" && src != "" {
			m[dst] = src
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// hostPathByMounts 按挂载映射翻译路径：取能匹配上的最长前缀，命中不了则原样返回
func hostPathByMounts(p string, mounts map[string]string) string {
	best := ""
	for dst := range mounts {
		if pathWithin(p, dst) && len(dst) > len(best) {
			best = dst
		}
	}
	if best == "" {
		return p
	}
	return rewritePrefix(p, best, mounts[best])
}

// pathWithin p 等于 base，或位于 base 之下
func pathWithin(p, base string) bool {
	if base == "" {
		return false
	}
	base = strings.TrimSuffix(base, "/")
	return p == base || strings.HasPrefix(p, base+"/")
}

// rewritePrefix 把 p 的 oldPrefix 换成 newPrefix；p 不在 oldPrefix 下时原样返回
func rewritePrefix(p, oldPrefix, newPrefix string) string {
	rel, err := filepath.Rel(oldPrefix, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return filepath.Join(newPrefix, rel)
}

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
