package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

// ServerHandler frps 服务端配置与进程管理
// 支持两种部署模式：
//   - local：面板本机一体化托管（默认，与 M1 行为一致）
//   - agent：由远端 Agent 托管，面板只负责生成配置与下发指令
type ServerHandler struct {
	mgr *frp.Manager
	hub *agenthub.Hub
}

// NewServerHandler 创建服务端处理器
func NewServerHandler(mgr *frp.Manager, hub *agenthub.Hub) *ServerHandler {
	return &ServerHandler{mgr: mgr, hub: hub}
}

// isAgentMode 判断是否由远端 Agent 托管
func isAgentMode(s *model.FrpsServer) bool {
	return s.DeployMode == "agent" && s.AgentID > 0
}

// List GET /api/servers
func (h *ServerHandler) List(c *gin.Context) {
	var list []model.FrpsServer
	database.DB.Order("id asc").Find(&list)
	for i := range list {
		list[i].Status = h.statusOf(&list[i])
	}
	c.JSON(http.StatusOK, list)
}

// Get GET /api/servers/:id
func (h *ServerHandler) Get(c *gin.Context) {
	var s model.FrpsServer
	if err := database.DB.First(&s, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}
	s.Status = h.statusOf(&s)
	c.JSON(http.StatusOK, s)
}

// Create POST /api/servers
func (h *ServerHandler) Create(c *gin.Context) {
	var s model.FrpsServer
	if err := c.ShouldBindJSON(&s); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	s.ID = 0
	if s.Name == "" {
		s.Name = "默认服务端"
	}
	if s.AuthToken == "" {
		s.AuthToken = randomToken(16)
	}
	if s.BindPort == 0 {
		s.BindPort = 7000
	}
	// Dashboard 默认不启用，但仍预置一个 8 位随机初始密码，开启时即可直接使用
	if s.DashboardPwd == "" {
		s.DashboardPwd = randomPassword(8)
	}
	if s.DeployMode == "" {
		s.DeployMode = "local"
	}
	// 同一台机器上端口不能重复，否则后启动的 frps 会因地址占用而失败
	if err := checkPortConflict(&s, 0); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := database.DB.Create(&s).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败：" + err.Error()})
		return
	}
	s.Status = h.statusOf(&s)
	c.JSON(http.StatusOK, s)
}

// Update PUT /api/servers/:id
func (h *ServerHandler) Update(c *gin.Context) {
	var old model.FrpsServer
	if err := database.DB.First(&old, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}

	var req model.FrpsServer
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	req.ID = old.ID
	req.CreatedAt = old.CreatedAt
	// 部署模式未显式指定时保持原值，避免被零值覆盖
	if req.DeployMode == "" {
		req.DeployMode = old.DeployMode
	}
	req.Status = old.Status

	if err := checkPortConflict(&req, req.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := database.DB.Save(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	req.Status = h.statusOf(&req)
	c.JSON(http.StatusOK, req)
}

// Delete DELETE /api/servers/:id
func (h *ServerHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	_ = h.mgr.Stop(uint(id))
	if err := database.DB.Delete(&model.FrpsServer{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// Preview GET /api/servers/:id/config 预览生成的 frps.json
func (h *ServerHandler) Preview(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var s model.FrpsServer
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}
	content, err := h.buildConfig(&s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成配置失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// Apply POST /api/servers/:id/apply 生成配置并生效（本地模式落盘重启；Agent 模式下发并确认）
func (h *ServerHandler) Apply(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var s model.FrpsServer
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}

	content, err := h.buildConfig(&s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成配置失败：" + err.Error()})
		return
	}

	// Agent 模式：生成版本 → 下发 → Agent 自行校验并在失败时回滚
	if isAgentMode(&s) {
		version := nextVersion(proto.TargetServer, s.ID)
		recordConfigVersion(proto.TargetServer, s.ID, version, content)

		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID:    s.AgentID,
			Type:       proto.CmdApply,
			TargetType: proto.TargetServer,
			TargetID:   s.ID,
			Payload:    content,
			Version:    version,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if res.Queued {
			c.JSON(http.StatusOK, gin.H{"message": res.Message, "queued": true, "version": version})
			return
		}
		status := "stopped"
		if res.Running {
			status = "running"
		}
		c.JSON(http.StatusOK, gin.H{
			"message":    res.Message,
			"ok":         res.OK,
			"rolledBack": res.RolledBack,
			"version":    version,
			"status":     status,
		})
		return
	}

	// 本地一体化模式
	if err := h.mgr.WriteConfig(uint(id), content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入配置失败：" + err.Error()})
		return
	}

	restarted := false
	if h.mgr.Running(uint(id)) {
		if err := h.mgr.Restart(uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重启 frps 失败：" + err.Error()})
			return
		}
		restarted = true
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "配置已生效" + map[bool]string{true: "，frps 已重启", false: ""}[restarted],
		"restarted": restarted,
		"status":    h.statusOf(&s),
	})
}

// Start POST /api/servers/:id/start
func (h *ServerHandler) Start(c *gin.Context) {
	h.lifecycle(c, proto.CmdStart)
}

// Stop POST /api/servers/:id/stop
func (h *ServerHandler) Stop(c *gin.Context) {
	h.lifecycle(c, proto.CmdStop)
}

// Restart POST /api/servers/:id/restart
func (h *ServerHandler) Restart(c *gin.Context) {
	h.lifecycle(c, proto.CmdRestart)
}

// lifecycle 统一处理启停重启：Agent 模式下发指令，本地模式直接操作进程
func (h *ServerHandler) lifecycle(c *gin.Context, cmdType string) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var s model.FrpsServer
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}

	if isAgentMode(&s) {
		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID:    s.AgentID,
			Type:       cmdType,
			TargetType: proto.TargetServer,
			TargetID:   s.ID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if res.Queued {
			c.JSON(http.StatusOK, gin.H{"message": res.Message, "queued": true})
			return
		}
		if !res.OK {
			c.JSON(http.StatusInternalServerError, gin.H{"error": res.Message})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": res.Message, "status": h.statusOf(&s), "running": res.Running})
		return
	}

	// 本地模式：启停前先刷新配置文件，避免读到旧配置
	content, err := h.buildConfig(&s)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成配置失败：" + err.Error()})
		return
	}
	if err := h.mgr.WriteConfig(uint(id), content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入配置失败：" + err.Error()})
		return
	}

	switch cmdType {
	case proto.CmdStart:
		if !h.mgr.IsInstalled() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未找到 frps 二进制，请先放置到 " + h.mgr.BinPath()})
			return
		}
		if err := h.mgr.Start(uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case proto.CmdStop:
		if err := h.mgr.Stop(uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case proto.CmdRestart:
		if err := h.mgr.Restart(uint(id)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "操作完成", "status": h.statusOf(&s)})
}

// Log GET /api/servers/:id/log 查看日志尾部
func (h *ServerHandler) Log(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var s model.FrpsServer
	if err := database.DB.First(&s, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}

	if isAgentMode(&s) {
		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID:    s.AgentID,
			Type:       proto.CmdLog,
			TargetType: proto.TargetServer,
			TargetID:   s.ID,
			TimeoutMs:  15000,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if res.Queued {
			c.JSON(http.StatusOK, gin.H{"content": "", "message": res.Message, "queued": true})
			return
		}
		c.JSON(http.StatusOK, gin.H{"content": res.Content, "running": res.Running})
		return
	}

	content, err := h.mgr.TailLog(uint(id), 64*1024)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取日志失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// buildConfig 生成 frps.json；Agent 模式下日志路径交由 Agent 决定
func (h *ServerHandler) buildConfig(s *model.FrpsServer) (string, error) {
	if isAgentMode(s) {
		return frp.BuildFrpsJSON(s, "")
	}
	return frp.BuildFrpsJSON(s, h.mgr.LogPath(s.ID))
}

// statusOf 计算运行状态：Agent 模式取心跳缓存，本地模式检查进程
func (h *ServerHandler) statusOf(s *model.FrpsServer) string {
	if isAgentMode(s) {
		if !h.hub.Online(s.AgentID) {
			return "agent_offline"
		}
		if st, ok := h.hub.State(s.AgentID, proto.TargetServer, s.ID); ok {
			if st.Running {
				return "running"
			}
			return "stopped"
		}
		if s.Status == "running" {
			return "running"
		}
		return "stopped"
	}

	if !h.mgr.IsInstalled() {
		return "not_installed"
	}
	if h.mgr.Running(s.ID) {
		return "running"
	}
	return "stopped"
}

// Versions GET /api/servers/:id/versions 查看历史配置版本
// Agent 托管时取 Agent 本地快照（可回滚），本机托管时返回面板侧记录
func (h *ServerHandler) Versions(c *gin.Context) {
	var s model.FrpsServer
	if err := database.DB.First(&s, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}

	dbList := loadVersionRecords(proto.TargetServer, s.ID)
	if !isAgentMode(&s) {
		c.JSON(http.StatusOK, gin.H{"fromAgent": false, "versions": dbList})
		return
	}
	if !h.hub.Online(s.AgentID) {
		c.JSON(http.StatusOK, gin.H{"fromAgent": false, "versions": dbList, "message": "Agent 离线，仅显示面板记录"})
		return
	}

	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    s.AgentID,
		Type:       proto.CmdVersions,
		TargetType: proto.TargetServer,
		TargetID:   s.ID,
		TimeoutMs:  15000,
	})
	if err != nil || res.Queued {
		c.JSON(http.StatusOK, gin.H{"fromAgent": false, "versions": dbList})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"fromAgent": true,
		"current":   res.Running,
		"versions":  mergeVersions(res.Versions, dbList),
	})
}

// Rollback POST /api/servers/:id/rollback 回滚到指定历史版本
// Agent 托管：下发 rollback 指令，Agent 用本地快照应用并做健康校验（失败会再回退）
// 本机托管：面板直接写入目标版本配置并重启 frps
func (h *ServerHandler) Rollback(c *gin.Context) {
	var s model.FrpsServer
	if err := database.DB.First(&s, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务端不存在"})
		return
	}
	var req struct {
		Version int `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定要回滚到的版本号"})
		return
	}

	target, err := findVersionRecord(proto.TargetServer, s.ID, req.Version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "面板中未找到该版本的配置记录"})
		return
	}

	version := nextVersion(proto.TargetServer, s.ID)
	recordConfigVersion(proto.TargetServer, s.ID, version, target.Content)

	if isAgentMode(&s) {
		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID:    s.AgentID,
			Type:       proto.CmdRollback,
			TargetType: proto.TargetServer,
			TargetID:   s.ID,
			Payload:    rollbackPayload(req.Version),
			Version:    version,
			TimeoutMs:  150000,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if res.Queued {
			c.JSON(http.StatusOK, gin.H{"message": res.Message, "queued": true, "version": version})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":    res.Message,
			"ok":         res.OK,
			"rolledBack": res.RolledBack,
			"unverified": res.Unverified,
			"version":    version,
			"status":     h.statusOf(&s),
		})
		return
	}

	// 本机一体化：直接写入并重启
	if err := h.mgr.WriteConfig(s.ID, target.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入配置失败：" + err.Error()})
		return
	}
	if h.mgr.Running(s.ID) {
		if err := h.mgr.Restart(s.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重启 frps 失败：" + err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已回滚到版本 v%d", req.Version),
		"ok":      true,
		"version": version,
		"status":  h.statusOf(&s),
	})
}

// hostKey 判断两个服务端是否落在同一台机器上（本机托管视为同一台；Agent 托管按 Agent 区分）
func hostKey(s *model.FrpsServer) string {
	if s.DeployMode == "agent" {
		if s.AgentID == 0 {
			return "agent:unassigned"
		}
		return "agent:" + strconv.FormatUint(uint64(s.AgentID), 10)
	}
	return "local"
}

// checkPortConflict 校验监听端口与 Dashboard 端口是否与同机上的其它服务端冲突
func checkPortConflict(s *model.FrpsServer, excludeID uint) error {
	var list []model.FrpsServer
	if err := database.DB.Find(&list).Error; err != nil {
		return nil
	}

	self := hostKey(s)
	for i := range list {
		other := list[i]
		if other.ID == excludeID || hostKey(&other) != self {
			continue
		}
		if s.BindPort != 0 && s.BindPort == other.BindPort {
			return fmt.Errorf("监听端口 %d 已被服务端「%s」占用，同一台机器上端口不能重复", s.BindPort, other.Name)
		}
		if dashboardOn(s) && dashboardOn(&other) && s.DashboardPort > 0 && s.DashboardPort == other.DashboardPort {
			return fmt.Errorf("Dashboard 端口 %d 已被服务端「%s」占用", s.DashboardPort, other.Name)
		}
	}
	return nil
}

func dashboardOn(s *model.FrpsServer) bool {
	return s.DashboardEnabled != nil && *s.DashboardEnabled
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "change-me-token"
	}
	return hex.EncodeToString(buf)
}

// randomPassword 生成可读性较好的随机密码，剔除 0/O/1/l/I 等易混淆字符
func randomPassword(n int) string {
	const charset = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, n)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "dfpanel-" + randomToken(4)
		}
		buf[i] = charset[idx.Int64()]
	}
	return string(buf)
}
