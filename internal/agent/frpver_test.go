package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"dfpanel/internal/distrib"
)

// docker 运行时的槽位必须是拷贝：软链里写的是 Agent 视角的目标路径，
// 宿主上的 docker daemon 解析不到（挂载源会变成空目录）
func TestActivateSlotDockerCopies(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "docker"}
	a := &Agent{cfg: cfg}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	ver := "0.62.1"

	if err := os.MkdirAll(cfg.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfg.BinDir(), distrib.BinaryName("frpc", ver, goos, goarch))
	if err := os.WriteFile(src, []byte("frpc-body"), 0o755); err != nil {
		t.Fatal(err)
	}

	slot := cfg.ContainerSlotPath("frpc", goos, goarch)
	if err := a.activateSlot("frpc", slot, ver, goos, goarch); err != nil {
		t.Fatalf("激活槽位失败：%v", err)
	}
	fi, err := os.Lstat(slot)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("docker 运行时不该用软链做槽位")
	}
	if body, err := os.ReadFile(slot); err != nil || string(body) != "frpc-body" {
		t.Fatalf("槽位内容不对：%q %v", string(body), err)
	}
	if v, err := os.ReadFile(cfg.ContainerVersionSidecar("frpc", goos, goarch)); err != nil || string(v) != ver+"\n" {
		t.Fatalf("版本落签不对：%q %v", string(v), err)
	}
}

// docker 下软链槽位要判成「不就绪」，好让它被重做成拷贝
func TestSlotReadyDockerRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上软链需要开发者模式，实现会退化为拷贝")
	}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	ver := "0.62.1"

	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "docker"}
	a := &Agent{cfg: cfg}
	if err := os.MkdirAll(cfg.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfg.BinDir(), distrib.BinaryName("frpc", ver, goos, goarch))
	if err := os.WriteFile(src, []byte("frpc-body"), 0o755); err != nil {
		t.Fatal(err)
	}

	slot := cfg.ContainerSlotPath("frpc", goos, goarch)
	if err := os.Symlink(src, slot); err != nil {
		t.Fatal(err)
	}
	if a.slotReady(slot) {
		t.Fatal("docker 下软链槽位不该算就绪（容器挂载解析不到）")
	}
	if a.slotsReady([]string{"frpc"}) {
		t.Fatal("slotsReady 也应当判为未就绪")
	}
	if err := a.activateSlot("frpc", slot, ver, goos, goarch); err != nil {
		t.Fatal(err)
	}
	if !a.slotReady(slot) || !a.slotsReady([]string{"frpc"}) {
		t.Fatal("重做成拷贝后应当就绪")
	}

	// process 运行时软链就是正常形态
	pc := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"}
	pa := &Agent{cfg: pc}
	if err := os.MkdirAll(pc.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	psrc := filepath.Join(pc.BinDir(), distrib.BinaryName("frpc", ver, goos, goarch))
	if err := os.WriteFile(psrc, []byte("frpc-body"), 0o755); err != nil {
		t.Fatal(err)
	}
	pslot := pc.BinaryPath("frpc")
	if err := os.Symlink(psrc, pslot); err != nil {
		t.Fatal(err)
	}
	if !pa.slotReady(pslot) {
		t.Fatal("process 下软链槽位应当就绪")
	}
}

// process 运行时仍用软链（Windows 上 Symlink 常不可用，退化为拷贝，跳过判断）
func TestActivateSlotProcessSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上软链需要开发者模式，实现会退化为拷贝")
	}
	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"}
	a := &Agent{cfg: cfg}
	goos, goarch := runtime.GOOS, runtime.GOARCH
	ver := "0.62.1"

	if err := os.MkdirAll(cfg.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(cfg.BinDir(), distrib.BinaryName("frpc", ver, goos, goarch))
	if err := os.WriteFile(src, []byte("frpc-body"), 0o755); err != nil {
		t.Fatal(err)
	}

	slot := cfg.BinaryPath("frpc")
	if err := a.activateSlot("frpc", slot, ver, goos, goarch); err != nil {
		t.Fatalf("激活槽位失败：%v", err)
	}
	fi, err := os.Lstat(slot)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("process 运行时应当用软链")
	}
}

// 目标版本和当前一致时应当直接跳过：切换要停实例、换槽位再拉起，docker 运行时还要重建容器
func TestSameAsCurrent(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frps", Runtime: "process"}
	a := &Agent{cfg: cfg}

	if a.sameAsCurrent([]string{"frps"}, "0.71.0") {
		t.Error("没有任何版本落签时不该判定为「已经是目标版本」")
	}

	if err := os.MkdirAll(cfg.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.VersionSidecar("frps"), []byte("0.71.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !a.sameAsCurrent([]string{"frps"}, "0.71.0") {
		t.Error("落签已是 0.71.0，应判定为无需切换")
	}
	if a.sameAsCurrent([]string{"frps"}, "0.70.0") {
		t.Error("目标版本不同时不该判定为无需切换")
	}
	if a.sameAsCurrent([]string{"frps", "frpc"}, "0.71.0") {
		t.Error("frpc 还没有落签，不能因为 frps 一致就整体跳过")
	}
}

// 槽位里已经有二进制时不该再去面板下载（ensureRuntimeBinary 每次启动/应用都会调用）
func TestEnsureRuntimeBinarySkipsWhenSlotReady(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frpc", Runtime: "process"}
	a := &Agent{cfg: cfg}

	slot := cfg.LocalBinaryPath("frpc")
	if err := os.WriteFile(slot, []byte("fake-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}

	created, err := a.ensureRuntimeBinary("frpc")
	if err != nil {
		t.Fatalf("槽位就绪时不该报错：%v", err)
	}
	if created {
		t.Error("槽位就绪时不该重建")
	}
}

// 没启用该角色时直接跳过，不做任何下载
func TestEnsureRuntimeBinarySkipsDisabledRole(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Roles: "frps", Runtime: "process"}
	a := &Agent{cfg: cfg}

	created, err := a.ensureRuntimeBinary("frpc")
	if err != nil {
		t.Fatalf("未启用角色时不该报错：%v", err)
	}
	if created {
		t.Error("未启用的角色不该补槽位")
	}
}
