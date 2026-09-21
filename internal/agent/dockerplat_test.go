package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeContainerPlatform(t *testing.T) {
	// docker info 返回的是 uname 风格，必须映射到 GOARCH，否则会去下载不存在的 release 包
	archCases := map[string]string{
		"x86_64":     "amd64",
		"amd64":      "amd64",
		"aarch64":    "arm64",
		"arm64":      "arm64",
		"arm64/v8":   "arm64",
		"armv7l":     "arm",
		"  X86_64  ": "amd64",
		"riscv64":    "",
	}
	for in, want := range archCases {
		if got := normalizeContainerArch(in); got != want {
			t.Errorf("normalizeContainerArch(%q) = %q，期望 %q", in, got, want)
		}
	}

	osCases := map[string]string{
		"linux":   "linux",
		"Linux":   "linux",
		"windows": "windows",
		"darwin":  "darwin",
		"plan9":   "",
	}
	for in, want := range osCases {
		if got := normalizeContainerOS(in); got != want {
			t.Errorf("normalizeContainerOS(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestContainerPaths(t *testing.T) {
	// 容器内挂载点必须是单层路径：Docker 挂载单个文件时不保证会创建多级父目录
	for _, kind := range []string{"frps", "frpc"} {
		p := ContainerBinaryInContainer(kind)
		if strings.Count(p, "/") != 1 {
			t.Errorf("%s 的容器内路径 %q 不是单层路径", kind, p)
		}
	}
	if ContainerBinaryInContainer("frps") == ContainerBinaryInContainer("frpc") {
		t.Error("frps 与 frpc 的容器内路径不能相同")
	}

	c := &Config{DataDir: filepath.Join(t.TempDir(), "agent")}
	slot := c.ContainerSlotPath("frps", "linux", "amd64")
	if !strings.HasSuffix(slot, filepath.Join("bin", "frps-container-linux-amd64")) {
		t.Errorf("容器槽位路径 = %q", slot)
	}
	// 容器槽位不能带 .exe：给容器的是 linux 二进制，且要和 Windows 宿主的 process 槽位区分开
	if strings.Contains(slot, ".exe") {
		t.Errorf("容器槽位不应带 .exe：%q", slot)
	}
	if slot == c.LocalBinaryPath("frps") {
		t.Error("容器槽位与 process 槽位不能是同一个文件")
	}
	if c.ContainerVersionSidecar("frps", "linux", "amd64") == c.VersionSidecar("frps") {
		t.Error("容器槽位的版本落签不能与 process 槽位共用")
	}
}
