package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/distrib"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
	"dfpanel/internal/setting"
)

// FrpHandler frp 版本管理与系统设置接口（JWT 鉴权）
//
// 版本粒度按 Agent 统一：一台机器上的 frps 与 frpc 共用一个 frp 版本；
// 面板本机托管的 frps 视为一个隐含的「本机目标」，版本在设置页中配置。
// 版本获取固定使用 GitHub 官方接口；下载支持配置镜像源模板。
type FrpHandler struct {
	cfg *config.Config
	mgr *frp.Manager
	hub *agenthub.Hub
}

// NewFrpHandler 创建 frp 版本管理处理器
func NewFrpHandler(cfg *config.Config, mgr *frp.Manager, hub *agenthub.Hub) *FrpHandler {
	return &FrpHandler{cfg: cfg, mgr: mgr, hub: hub}
}

// ---------- 系统设置 ----------

// GetSettings GET /api/settings
func (h *FrpHandler) GetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, setting.Load(h.cfg.FRPDownloadBase))
}

// SaveSettings POST /api/settings
func (h *FrpHandler) SaveSettings(c *gin.Context) {
	var req struct {
		FrpDownloadBase   string `json:"frpDownloadBase"`
		FrpManualVersions string `json:"frpManualVersions"`
		PanelFrpVersion   string `json:"panelFrpVersion"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	if err := setting.Save(setting.Values{
		FrpDownloadBase:   req.FrpDownloadBase,
		FrpManualVersions: req.FrpManualVersions,
		PanelFrpVersion:   req.PanelFrpVersion,
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, setting.Load(h.cfg.FRPDownloadBase))
}

// ListVersions GET /api/frp-versions 可选版本列表（GitHub 官方接口 + 手填兜底 + 本机已缓存）
func (h *FrpHandler) ListVersions(c *gin.Context) {
	available, err := distrib.ListVersions()
	msg := ""
	if err != nil {
		// 版本列表拿不到不阻塞功能：前端可手填版本号完成下载
		msg = "读取官方版本列表失败（可手填版本号继续）：" + err.Error()
		available = []string{}
	}

	cached := h.mgr.CachedVersions()
	manual := setting.ManualVersions()
	resp := gin.H{
		"available": available,
		"manual":    manual,
		"cached":    cached,
		"merged":    distrib.MergeVersions(distrib.MergeVersions(available, manual), cached),
		"api":       distrib.VersionListAPI,
		"download":  setting.Load(h.cfg.FRPDownloadBase).FrpDownloadBase,
	}
	if len(available) > 0 {
		resp["latest"] = available[0]
	}
	if msg != "" {
		resp["message"] = msg
	}
	c.JSON(http.StatusOK, resp)
}

// ---------- 面板本机 ----------

// LocalFrp GET /api/frp/local 面板本机 frps 的版本状态
func (h *FrpHandler) LocalFrp(c *gin.Context) {
	expected := setting.Get(setting.KeyPanelFrpVersion)
	active := h.mgr.ActiveVersion()
	cached := h.mgr.CachedVersions()
	c.JSON(http.StatusOK, gin.H{
		"expected":   expected,
		"active":     active,
		"cached":     cached,
		"updatable":  expected != "" && active != "" && expected != active,
		"outdated":   latestIsNewer(active, cached),
		"serial":     "local",
		"displayName": "面板本机",
		"instances":  countLocalTargets(),
	})
}

// LocalFrpDownload POST /api/frp/local/download 下载指定版本到面板本机（不切换、不重启）
func (h *FrpHandler) LocalFrpDownload(c *gin.Context) {
	version, ok := h.bindVersion(c)
	if !ok {
		return
	}
	base := setting.Load(h.cfg.FRPDownloadBase).FrpDownloadBase
	path, err := h.mgr.EnsureVersioned(version, base)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "下载失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "已下载 frps " + distrib.ParseBinaryName(baseName(path), "frps"),
		"active":  h.mgr.ActiveVersion(),
		"cached":  h.mgr.CachedVersions(),
	})
}

// LocalFrpActivate POST /api/frp/local/activate 切换面板本机 frps 版本并重启全部本机实例
func (h *FrpHandler) LocalFrpActivate(c *gin.Context) {
	version, ok := h.bindVersion(c)
	if !ok {
		return
	}
	if actionable, err := h.resolveVersion(version); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	} else {
		version = actionable
	}

	base := setting.Load(h.cfg.FRPDownloadBase).FrpDownloadBase
	if _, err := h.mgr.EnsureVersioned(version, base); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "准备 frps " + version + " 失败：" + err.Error()})
		return
	}

	// 本机托管实例（deployMode != agent）
	var servers []model.FrpsServer
	database.DB.Where("deploy_mode IS NULL OR deploy_mode <> ? OR agent_id = 0", "agent").Find(&servers)

	oldVersion := h.mgr.ActiveVersion()
	runningBefore := map[uint]bool{}
	for _, s := range servers {
		runningBefore[s.ID] = h.mgr.Running(s.ID)
	}

	// 1. 停掉全部本机实例，释放二进制占用
	stopErrs := []string{}
	for _, s := range servers {
		if !runningBefore[s.ID] {
			continue
		}
		if err := h.mgr.Stop(s.ID); err != nil {
			stopErrs = append(stopErrs, s.Name+": "+err.Error())
		}
	}
	if len(stopErrs) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "切换前停止实例失败，已放弃操作：" + strings.Join(stopErrs, "；"),
		})
		return
	}

	// 2. 切换 active 槽位
	if _, err := h.mgr.Activate(version); err != nil {
		h.restartLocal(servers, runningBefore)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "切换 frps " + version + " 失败，已回退：" + err.Error(),
			"active": h.mgr.ActiveVersion(),
		})
		return
	}

	// 3. 拉起之前在跑的实例，并做一次轻量存活确认
	failures := h.restartLocal(servers, runningBefore)
	if len(failures) > 0 {
		// 4. 明确失败：切回旧版本并恢复运行
		for _, s := range servers {
			if runningBefore[s.ID] {
				_ = h.mgr.Stop(s.ID)
			}
		}
		restoreErr := ""
		if oldVersion == "" {
			h.mgr.RemoveActive()
		} else if _, err := h.mgr.Activate(oldVersion); err != nil {
			restoreErr = err.Error()
		}
		again := h.restartLocal(servers, runningBefore)

		msg := "frps " + version + " 切换失败，已回退到 " + emptyVersion(oldVersion) + "：" + strings.Join(failures, "；")
		if restoreErr != "" {
			msg += "；回退二进制出错：" + restoreErr
		}
		if len(again) > 0 {
			msg += "；回退后仍有实例未就绪：" + strings.Join(again, "；")
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":         false,
			"rolledBack": true,
			"message":    msg,
			"active":     h.mgr.ActiveVersion(),
			"cached":     h.mgr.CachedVersions(),
		})
		return
	}

	restarted := countTrue(runningBefore)
	detail := "共重启 " + strconv.Itoa(restarted) + " 个实例"
	if restarted == 0 {
		detail = "本机没有实例需要重启"
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "已切换面板本机 frps 到 " + version + "，" + detail,
		"active":  h.mgr.ActiveVersion(),
		"cached":  h.mgr.CachedVersions(),
	})
}

// restartLocal 按切换前的运行状态拉起本机实例，返回未就绪的实例说明
func (h *FrpHandler) restartLocal(servers []model.FrpsServer, runningBefore map[uint]bool) []string {
	failed := []string{}
	for _, s := range servers {
		if !runningBefore[s.ID] {
			continue
		}
		if err := h.mgr.Start(s.ID); err != nil {
			failed = append(failed, s.Name+" 启动失败："+err.Error())
			continue
		}
		// 轻量存活确认：起得来但立刻退出的二进制（如架构不匹配）要能识别出来
		time.Sleep(1500 * time.Millisecond)
		if !h.mgr.Running(s.ID) {
			failed = append(failed, s.Name+" 启动后立即退出，请查看日志")
		}
	}
	return failed
}

// ---------- Agent ----------

// AgentFrp GET /api/agents/:id/frp 某 Agent 的 frp 版本状态
func (h *FrpHandler) AgentFrp(c *gin.Context) {
	var a model.Agent
	if err := database.DB.First(&a, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}

	online := h.hub.Online(a.ID)
	active := a.FRPInstalledVersion
	cached := splitCSV(a.FRPCachedVersions)
	message := ""

	// 在线时顺带拉一次实时状态，保证展示的是 Agent 的真实情况
	if online {
		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID:    a.ID,
			Type:       proto.CmdFrpStatus,
			TargetType: proto.TargetAgent,
			TimeoutMs:  15000,
		})
		if err == nil && res.OK {
			active = res.FrpVersion
			if len(res.FrpCached) > 0 {
				cached = res.FrpCached
			}
			// docker 运行时若 docker 不可用，Agent 会把原因带回来
			message = res.Message
			_ = database.DB.Model(&model.Agent{}).Where("id = ?", a.ID).
				Updates(map[string]any{
					"frp_installed_version": active,
					"frp_cached_versions":   strings.Join(cached, ","),
				}).Error
		}
	}

	runtimeKind := a.Runtime
	if runtimeKind == "" {
		runtimeKind = "process"
	}
	c.JSON(http.StatusOK, gin.H{
		"online":    online,
		"expected":  a.FRPVersion,
		"active":    active,
		"cached":    cached,
		"updatable": a.FRPVersion != "" && active != "" && a.FRPVersion != active,
		"outdated":  latestIsNewer(active, cached),
		"updatedAt": a.FRPUpdatedAt,
		"runtime":   runtimeKind,
		"message":   message,
		"instances": countAgentTargets(a.ID),
	})
}

// AgentFrpDownload POST /api/agents/:id/frp/download 让 Agent 下载指定版本（不切换、不重启）
func (h *FrpHandler) AgentFrpDownload(c *gin.Context) {
	a, version, ok := h.agentAndVersion(c)
	if !ok {
		return
	}
	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    a.ID,
		Type:       proto.CmdFrpDownload,
		TargetType: proto.TargetAgent,
		Payload:    frpVersionPayload(version),
		TimeoutMs:  int((4 * time.Minute).Milliseconds()),
	})
	// 下载不改变期望版本，故不写 frp_version
	h.writeAgentResult(c, a.ID, res, err, "")
}

// AgentFrpActivate POST /api/agents/:id/frp/activate 切换版本并重启该 Agent 上全部托管实例
func (h *FrpHandler) AgentFrpActivate(c *gin.Context) {
	a, version, ok := h.agentAndVersion(c)
	if !ok {
		return
	}
	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    a.ID,
		Type:       proto.CmdFrpActivate,
		TargetType: proto.TargetAgent,
		Payload:    frpVersionPayload(version),
		// 切换含「停全部实例 + 逐个健康探测」，等待窗口按较宽的值给
		TimeoutMs: int((6 * time.Minute).Milliseconds()),
	})
	h.writeAgentResult(c, a.ID, res, err, version)
}

// agentAndVersion 取出 Agent 与解析后的具体版本号（latest 在面板侧解析，避免 Agent 各自漂移）
func (h *FrpHandler) agentAndVersion(c *gin.Context) (*model.Agent, string, bool) {
	var a model.Agent
	if err := database.DB.First(&a, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return nil, "", false
	}
	version, ok := h.bindVersion(c)
	if !ok {
		return nil, "", false
	}
	resolved, err := h.resolveVersion(version)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return nil, "", false
	}
	return &a, resolved, true
}

// writeAgentResult 落库并回传 Agent 的版本操作结果
func (h *FrpHandler) writeAgentResult(c *gin.Context, agentID uint, res dispatchResult, err error, expect string) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if res.Queued {
		c.JSON(http.StatusOK, gin.H{"queued": true, "message": res.Message})
		return
	}

	updates := map[string]any{}
	if res.FrpVersion != "" {
		updates["frp_installed_version"] = res.FrpVersion
	}
	if len(res.FrpCached) > 0 {
		updates["frp_cached_versions"] = strings.Join(res.FrpCached, ",")
	}
	if res.OK && expect != "" {
		updates["frp_version"] = expect
		now := time.Now()
		updates["frp_updated_at"] = now
	}
	if len(updates) > 0 {
		_ = database.DB.Model(&model.Agent{}).Where("id = ?", agentID).Updates(updates).Error
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":         res.OK,
		"message":    res.Message,
		"rolledBack": res.RolledBack,
		"active":     res.FrpVersion,
		"cached":     res.FrpCached,
		"expect":     expect,
	})
}

// ---------- 公共辅助 ----------

// countLocalTargets 面板本机托管的服务端数量（交给 Agent 的不算）
func countLocalTargets() int {
	var n int64
	database.DB.Model(&model.FrpsServer{}).
		Where("deploy_mode IS NULL OR deploy_mode <> ? OR agent_id = 0", "agent").Count(&n)
	return int(n)
}

// countAgentTargets 该 Agent 名下已托管的实例数量（服务端 + 节点）。
// 为 0 时切换版本只是换二进制，没有任何服务会被重启，界面上也就不该写「重启」。
func countAgentTargets(agentID uint) int {
	var servers, nodes int64
	database.DB.Model(&model.FrpsServer{}).Where("agent_id = ?", agentID).Count(&servers)
	database.DB.Model(&model.Node{}).Where("agent_id = ?", agentID).Count(&nodes)
	return int(servers + nodes)
}

// bindVersion 解析请求体中的 version 字段
func (h *FrpHandler) bindVersion(c *gin.Context) (string, bool) {
	var req struct {
		Version string `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return "", false
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定版本号，可用 latest"})
		return "", false
	}
	if version != distrib.LatestTag && !distrib.ValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本号格式不合法：" + version})
		return "", false
	}
	return version, true
}

// resolveVersion 把 latest 解析为具体版本号
func (h *FrpHandler) resolveVersion(version string) (string, error) {
	if version != distrib.LatestTag {
		return version, nil
	}
	return distrib.LatestVersion()
}

func frpVersionPayload(version string) string {
	return `{"version":"` + version + `"}`
}

func splitCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func baseName(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

func countTrue(m map[uint]bool) int {
	n := 0
	for _, v := range m {
		if v {
			n++
		}
	}
	return n
}

func emptyVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "未知版本"
	}
	return v
}

// latestIsNewer 已缓存/可用版本里有比当前 active 更新的版本时返回 true，用于界面提示「可更新」
func latestIsNewer(active string, cached []string) bool {
	if active == "" {
		return len(cached) > 0
	}
	for _, v := range cached {
		if compareSemver(v, active) > 0 {
			return true
		}
	}
	return false
}

func compareSemver(a, b string) int {
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
