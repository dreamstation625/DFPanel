package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"dfpanel/internal/distrib"
	"dfpanel/internal/proto"
)

// frpVersionPayload frp 版本相关指令的 payload
type frpVersionPayload struct {
	Version string `json:"version"`
}

// enabledKinds 该 Agent 启用的 frp 角色，顺序固定便于结果展示
func (a *Agent) enabledKinds() []string {
	out := make([]string, 0, 2)
	for _, k := range []string{"frps", "frpc"} {
		if a.cfg.HasRole(k) {
			out = append(out, k)
		}
	}
	return out
}

// frpPlatform 版本替换所针对的 frp 二进制平台。
//
// process 运行时 = Agent 宿主平台；docker 运行时 = **容器平台**（Docker Desktop 上跑的是
// Linux 容器，必须取 linux/amd64 而不是宿主的 windows/amd64）。
func (a *Agent) frpPlatform() (string, string, error) {
	if a.cfg.Runtime == "docker" {
		return containerPlatform()
	}
	return runtime.GOOS, runtime.GOARCH, nil
}

// slotPath 当前运行时的 active 槽位路径
func (a *Agent) slotPath(kind string) (string, error) {
	goos, goarch, err := a.frpPlatform()
	if err != nil {
		return "", err
	}
	if a.cfg.Runtime == "docker" {
		return a.cfg.ContainerSlotPath(kind, goos, goarch), nil
	}
	return a.cfg.LocalBinaryPath(kind), nil
}

// slotVersion 槽位当前生效的 frp 版本（空表示未接管）
func (a *Agent) slotVersion(kind string) string {
	goos, goarch, err := a.frpPlatform()
	if err != nil {
		return ""
	}
	if a.cfg.Runtime == "docker" {
		return a.cfg.ContainerSlotVersion(kind, goos, goarch)
	}
	return a.cfg.BinaryVersion(kind)
}

// sameAsCurrent 所有启用角色的 active 槽位都已经是目标版本时返回 true
func (a *Agent) sameAsCurrent(kinds []string, version string) bool {
	for _, kind := range kinds {
		if a.slotVersion(kind) != version {
			return false
		}
	}
	return true
}

// slotCached 该平台在本地已缓存的版本
func (a *Agent) slotCached(kind string) []string {
	goos, goarch, err := a.frpPlatform()
	if err != nil {
		return nil
	}
	if a.cfg.Runtime == "docker" {
		return a.cfg.ContainerCachedVersions(kind, goos, goarch)
	}
	return a.cfg.CachedVersions(kind)
}

// frpPlatformError 平台探测失败原因（为空表示正常）。
// docker 运行时时若 docker 不可用，版本状态会读不出来，需要把原因透出给面板而不是静默显示未知。
func (a *Agent) frpPlatformError() string {
	if a.cfg.Runtime != "docker" {
		return ""
	}
	if _, _, err := containerPlatform(); err != nil {
		return err.Error()
	}
	return ""
}

// versionedPath 某个具体版本在该平台下的版本化文件路径
func (a *Agent) versionedPath(kind, version, goos, goarch string) string {
	return filepath.Join(a.cfg.BinDir(), distrib.BinaryName(kind, version, goos, goarch))
}

// frpStatus 当前 active 版本与本地已缓存的版本
//
// 版本粒度按 Agent 统一：frps 与 frpc 共用一个版本，因此这里取两者交集信息，
// active 版本以先能读到的角色为准（正常情况下两者一致）。
func (a *Agent) frpStatus() (string, []string) {
	active := ""
	seen := map[string]bool{}
	cached := []string{}
	for _, kind := range a.enabledKinds() {
		if active == "" {
			active = a.slotVersion(kind)
		}
		for _, v := range a.slotCached(kind) {
			if !seen[v] {
				seen[v] = true
				cached = append(cached, v)
			}
		}
	}
	sort.Slice(cached, func(i, j int) bool { return compareVer(cached[i], cached[j]) > 0 })
	return active, cached
}

// hostedTargets 扫描数据目录，列出该 Agent 实际托管的实例
func (a *Agent) hostedTargets() []Target {
	entries, err := os.ReadDir(a.cfg.DataDir)
	if err != nil {
		return nil
	}
	out := []Target{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		var kind, typ string
		switch {
		case strings.HasPrefix(name, "frps-"):
			kind, typ = "frps", proto.TargetServer
		case strings.HasPrefix(name, "frpc-"):
			kind, typ = "frpc", proto.TargetNode
		default:
			continue
		}
		if !a.cfg.HasRole(kind) {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, kind+"-"), ".json"), 10, 64)
		if err != nil {
			continue
		}
		out = append(out, Target{Type: typ, ID: uint(id)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// resetControllers 清空控制器缓存，使其按新的 active 二进制路径重建。
// 调用前必须确保相关实例已全部停止，否则会丢失进程句柄。
func (a *Agent) resetControllers() {
	a.mu.Lock()
	a.ctrls = make(map[string]*Controller)
	a.mu.Unlock()
}

// ensureVersionedBinary 确保指定版本的二进制已落盘，缺失则从面板拉取（按当前运行时的平台）
func (a *Agent) ensureVersionedBinary(kind, version string) (string, string, error) {
	goos, goarch, err := a.frpPlatform()
	if err != nil {
		return "", "", err
	}
	dest := a.versionedPath(kind, version, goos, goarch)
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, version, nil
	}
	_, resolved, err := a.client.DownloadBinary(kind, version, goos, goarch)
	if err != nil {
		return "", "", err
	}
	if resolved != version {
		// 面板解析出的版本与请求不一致（例如请求 latest），以面板返回的为准
		dest = a.versionedPath(kind, resolved, goos, goarch)
	}
	return dest, resolved, nil
}

// ensureRuntimeBinary 确保当前运行时需要的 frp 二进制就绪，缺失时从面板拉取。
//
// 镜像与安装包都不再内置 frp，二进制一律由面板下发：
//   process → 版本化二进制切到 <dataDir>/<kind>[.exe] 槽位
//   docker  → 切到容器槽位（<binDir>/<kind>-container-<os>-<arch>），启动时挂进容器
//
// 返回 true 表示这次新补了槽位 —— 控制器里缓存的 spec 还是「没有二进制」的老样子，
// 调用方需要重建控制器才能带上挂载信息。
func (a *Agent) ensureRuntimeBinary(kind string) (bool, error) {
	if !a.cfg.HasRole(kind) {
		return false, nil
	}
	slot, err := a.slotPath(kind)
	if err != nil {
		return false, err
	}
	if fi, statErr := os.Stat(slot); statErr == nil && fi.Size() > 0 {
		return false, nil
	}
	goos, goarch, err := a.frpPlatform()
	if err != nil {
		return false, err
	}
	_, resolved, err := a.ensureVersionedBinary(kind, a.cfg.FRPVersion)
	if err != nil {
		return false, fmt.Errorf("从面板获取 %s 二进制失败：%v", kind, err)
	}
	if _, err := distrib.ActivateBinary(a.cfg.BinDir(), slot, kind, resolved, goos, goarch); err != nil {
		return false, fmt.Errorf("激活 %s %s 失败：%v", kind, resolved, err)
	}
	return true, nil
}

// handleFrpActivate 把指定版本切为 active 槽位，并重启该 Agent 上全部托管实例。
//
// 由于 frps 与 frpc 共用一个版本，切换是「整体」行为：
// 先停全部实例（Windows 下运行中的 exe 被占用无法替换；容器则顺带释放挂载）
// → 换槽位 → 逐个拉起并做健康探测；任一实例判定为明确故障则整体切回旧版本并重启，
// 保证不会把服务留在坏版本上。
//
// process 与 docker 两条路径共用同一套流程，差别只在槽位位置与二进制平台。
func (a *Agent) handleFrpActivate(cmd proto.CommandData) proto.ResultData {
	var req frpVersionPayload
	if cmd.Payload != "" {
		_ = json.Unmarshal([]byte(cmd.Payload), &req)
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		return proto.ResultData{Message: "未指定要切换到的 frp 版本"}
	}
	kinds := a.enabledKinds()
	if len(kinds) == 0 {
		return proto.ResultData{Message: "该 Agent 未启用 frps / frpc 角色，无需切换 frp 版本"}
	}
	pgoos, pgoarch, err := a.frpPlatform()
	if err != nil {
		return proto.ResultData{Message: err.Error()}
	}

	// 目标版本的二进制必须就绪；缺失则先补齐（activate 允许自给自足）
	concrete := version
	for _, kind := range kinds {
		dest, ver, dErr := a.ensureVersionedBinary(kind, version)
		if dErr != nil {
			return proto.ResultData{Message: fmt.Sprintf("准备 %s %s 失败：%v", kind, version, dErr)}
		}
		concrete = ver
		_ = dest
	}

	// 版本没变化就别动：切换要停实例、换槽位再拉起，docker 运行时还要重建容器，白折腾一遍
	if a.sameAsCurrent(kinds, concrete) {
		active, cached := a.frpStatus()
		return proto.ResultData{
			OK:         true,
			Message:    fmt.Sprintf("当前已经是 frp %s，没有变化", concrete),
			FrpVersion: active,
			FrpCached:  cached,
		}
	}

	oldVersions := map[string]string{}
	slots := map[string]string{}
	for _, kind := range kinds {
		oldVersions[kind] = a.slotVersion(kind)
		slot, sErr := a.slotPath(kind)
		if sErr != nil {
			return proto.ResultData{Message: sErr.Error()}
		}
		slots[kind] = slot
	}

	targets := a.hostedTargets()
	runningBefore := map[string]bool{}
	for _, t := range targets {
		runningBefore[targetKey(t)] = a.controller(t).Running()
	}

	// 1. 先停全部实例，释放文件占用 / 解除容器挂载
	stopErrs := []string{}
	for _, t := range targets {
		if err := a.controller(t).Stop(); err != nil {
			stopErrs = append(stopErrs, fmt.Sprintf("%s-%d: %v", kindOf(t), t.ID, err))
		}
	}
	if len(stopErrs) > 0 {
		return proto.ResultData{Message: "切换前停止实例失败，已放弃操作：" + strings.Join(stopErrs, "；")}
	}

	// 2. 切换 active 槽位（停掉后重建控制器，避免持有旧的挂载/路径）
	a.resetControllers()
	for _, kind := range kinds {
		if _, err := distrib.ActivateBinary(a.cfg.BinDir(), slots[kind], kind, concrete, pgoos, pgoarch); err != nil {
			a.restoreActive(oldVersions, slots, kinds, pgoos, pgoarch)
			a.restartTargets(targets, runningBefore)
			return proto.ResultData{
				Message:   fmt.Sprintf("切换 frp %s 失败，已回退：%v", concrete, err),
				FrpCached: a.cachedOnly(),
			}
		}
	}

	// 3. 逐个拉起并做健康探测
	failures := []string{}
	for _, t := range targets {
		if !runningBefore[targetKey(t)] {
			continue
		}
		if reason, ok := a.startAndCheck(t); !ok {
			failures = append(failures, reason)
		}
	}

	active, cached := a.frpStatus()
	if len(failures) > 0 {
		// 4. 明确故障：整体切回旧版本并恢复运行
		for _, t := range targets {
			_ = a.controller(t).Stop()
		}
		a.resetControllers()
		restoreErr := a.restoreActive(oldVersions, slots, kinds, pgoos, pgoarch)
		restarted := a.restartTargets(targets, runningBefore)

		msg := fmt.Sprintf("frp %s 切换失败，已回退到 %s：%s", concrete, describeVersions(oldVersions, kinds), strings.Join(failures, "；"))
		if restoreErr != nil {
			msg += "；回退二进制时出错：" + restoreErr.Error()
		}
		if restarted != "" {
			msg += "；" + restarted
		}
		return proto.ResultData{
			OK:                 false,
			RolledBack:         true,
			Message:            msg,
			FrpVersion:         a.slotVersion(kinds[0]),
			FrpCached:          cached,
			RollbackFrpVersion: describeVersions(oldVersions, kinds),
		}
	}

	mode := ""
	if a.cfg.Runtime == "docker" {
		mode = "（容器将挂载该二进制运行，镜像 tag 不变）"
	}
	restarted := countRunning(runningBefore)
	detail := fmt.Sprintf("共重启 %d 个实例", restarted)
	if restarted == 0 {
		detail = "没有实例需要重启"
	}
	return proto.ResultData{
		OK:         true,
		Message:    fmt.Sprintf("已切换到 frp %s，%s%s", concrete, detail, mode),
		FrpVersion: active,
		FrpCached:  cached,
	}
}

// confirmRunning 启动/重启后确认进程没有立刻退出，返回空字符串表示正常。
//
// frp 因端口占用、配置非法等原因起不来时会立即退出，而 Start 本身是成功的；
// 不确认的话面板会收到「已启动」，界面上看着像启成功了。日志尾部会一起带回去。
func (a *Agent) confirmRunning(t Target) string {
	ctrl := a.controller(t)
	kind := kindOf(t)
	for i := 0; i < 5; i++ {
		time.Sleep(300 * time.Millisecond)
		if !ctrl.Running() {
			logs, _ := ctrl.Logs(healthLogScanBytes)
			return fmt.Sprintf("%s-%d 启动后立即退出（%s）", kind, t.ID, lastLines(logs, 3))
		}
	}
	return ""
}

// startAndCheck 启动单个实例并做健康探测，返回 (失败原因, 是否通过)
func (a *Agent) startAndCheck(t Target) (string, bool) {
	ctrl := a.controller(t)
	kind := kindOf(t)
	cfgPath := a.cfg.ConfigPath(t)

	baseOffset := ctrl.LogSize()
	if err := ctrl.Start(); err != nil {
		return fmt.Sprintf("%s-%d 启动失败：%v", kind, t.ID, err), false
	}
	h := waitHealthy(ctrl, kind, cfgPath, baseOffset, defaultHealthTimeout)
	if h.Verdict == VerdictFail {
		return fmt.Sprintf("%s-%d %s", kind, t.ID, h.Reason), false
	}
	return "", true
}

// restartTargets 按切换前的运行状态重新拉起实例，返回非空字符串表示有部分失败
func (a *Agent) restartTargets(targets []Target, runningBefore map[string]bool) string {
	failed := []string{}
	for _, t := range targets {
		if !runningBefore[targetKey(t)] {
			continue
		}
		if reason, ok := a.startAndCheck(t); !ok {
			failed = append(failed, reason)
		}
	}
	if len(failed) == 0 {
		return ""
	}
	return "回滚后仍有实例未就绪：" + strings.Join(failed, "；")
}

// restoreActive 把 active 槽位恢复到切换前的版本；原先没有槽位的则清除（回到镜像/系统自带）
func (a *Agent) restoreActive(oldVersions, slots map[string]string, kinds []string, goos, goarch string) error {
	errs := []string{}
	for _, kind := range kinds {
		old := oldVersions[kind]
		slot := slots[kind]
		if old == "" {
			_ = os.Remove(slot)
			// 容器槽位还带一个版本落签文件，一并清掉，避免版本号残留
			_ = os.Remove(a.cfg.ContainerVersionSidecar(kind, goos, goarch))
			if a.cfg.Runtime != "docker" {
				_ = os.Remove(a.cfg.VersionSidecar(kind))
			}
			continue
		}
		if _, err := distrib.ActivateBinary(a.cfg.BinDir(), slot, kind, old, goos, goarch); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", kind, err))
		}
	}
	a.resetControllers()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	return nil
}

func (a *Agent) cachedOnly() []string {
	_, cached := a.frpStatus()
	return cached
}

func describeVersions(oldVersions map[string]string, kinds []string) string {
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, emptyAs(oldVersions[kind], "镜像自带"))
	}
	return strings.Join(parts, " / ")
}

func countRunning(m map[string]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

func emptyAs(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func compareVer(a, b string) int {
	as, bs := strings.SplitN(a, ".", 3), strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}
