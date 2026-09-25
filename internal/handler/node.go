package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

// NodeHandler 节点（frpc）管理：节点自身永远由远端 Agent 托管
type NodeHandler struct {
	hub *agenthub.Hub
	cfg *config.Config
}

// NewNodeHandler 创建节点处理器
func NewNodeHandler(hub *agenthub.Hub, cfg *config.Config) *NodeHandler {
	return &NodeHandler{hub: hub, cfg: cfg}
}

// List GET /api/nodes
func (h *NodeHandler) List(c *gin.Context) {
	var list []model.Node
	database.DB.Order("id asc").Find(&list)
	for i := range list {
		list[i].Status = h.statusOf(list[i])
	}
	c.JSON(http.StatusOK, list)
}

// Get GET /api/nodes/:id
func (h *NodeHandler) Get(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	n.Status = h.statusOf(n)
	c.JSON(http.StatusOK, n)
}

// Create POST /api/nodes
func (h *NodeHandler) Create(c *gin.Context) {
	var n model.Node
	if err := c.ShouldBindJSON(&n); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	// 客户端节点必须由 Agent 承载，因此要求先有可用的 Agent
	if err := requireManagedAgent(n.AgentID, 0); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	n.ID = 0
	if n.Name == "" {
		n.Name = "节点-" + time.Now().Format("0102150405")
	}
	if n.NodeKey == "" {
		n.NodeKey = randomToken(16)
	}
	if n.Secret == "" {
		n.Secret = randomToken(32)
	}
	if err := database.DB.Create(&n).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败：" + err.Error()})
		return
	}
	n.Status = h.statusOf(n)
	c.JSON(http.StatusOK, n)
}

// requireManagedAgent 校验节点绑定的托管 Agent 是否可用。
//
// 客户端节点（frpc）永远由 Agent 承载，没有 Agent 就没有任何机器能跑这个 frpc，
// 所以创建 / 保存节点时必须已经存在一个具备 frpc 角色的 Agent。
func requireManagedAgent(agentID, excludeNodeID uint) error {
	if agentID == 0 {
		return errString("请先选择托管 Agent：客户端节点必须由 Agent 承载，请先到「Agent 管理」创建并在目标机器上安装 Agent")
	}
	var a model.Agent
	if err := database.DB.First(&a, agentID).Error; err != nil {
		return errString("所选的 Agent 不存在，请重新选择")
	}
	if !a.HasRole("frpc") {
		return errString("Agent「" + a.Name + "」不是客户端 Agent")
	}
	var count int64
	if err := database.DB.Model(&model.Node{}).Where("agent_id = ? AND id <> ?", agentID, excludeNodeID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errString("该客户端 Agent 已绑定一个节点，请选择未绑定的 Agent")
	}
	return nil
}

// Update PUT /api/nodes/:id
func (h *NodeHandler) Update(c *gin.Context) {
	var old model.Node
	if err := database.DB.First(&old, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	var req model.Node
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	// 与创建保持一致：保存时也必须绑定可用的 Agent（顺带让历史遗留的未绑定节点补绑）
	if old.AgentID != 0 && req.AgentID != old.AgentID {
		c.JSON(http.StatusConflict, gin.H{"error": "已创建节点不能更换 Agent；请停止并删除旧节点后重新创建"})
		return
	}
	if err := requireManagedAgent(req.AgentID, old.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ID = old.ID
	req.CreatedAt = old.CreatedAt
	req.NodeKey = old.NodeKey
	req.Secret = old.Secret
	req.Status = old.Status
	req.ManualStopped = old.ManualStopped

	if err := database.DB.Save(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	req.Status = h.statusOf(req)
	c.JSON(http.StatusOK, req)
}

// Delete DELETE /api/nodes/:id
func (h *NodeHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var n model.Node
	if err := database.DB.First(&n, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	queued := false
	if n.AgentID != 0 {
		res, err := dispatch(h.hub, &model.AgentCommand{
			AgentID: n.AgentID, Type: proto.CmdUnassign, TargetType: proto.TargetNode, TargetID: n.ID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !res.OK && !res.Queued {
			c.JSON(http.StatusInternalServerError, gin.H{"error": res.Message})
			return
		}
		queued = res.Queued
	}
	if err := database.DB.Delete(&n).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除", "cleanupQueued": queued})
}

// Preview GET /api/nodes/:id/config 预览 frpc.json
func (h *NodeHandler) Preview(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	content, err := h.buildConfig(&n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成配置失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// Apply POST /api/nodes/:id/apply 生成配置并下发给托管 Agent（失败由 Agent 自动回滚）
func (h *NodeHandler) Apply(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	if n.AgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先为该节点指定托管 Agent"})
		return
	}

	content, err := h.buildConfig(&n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成配置失败：" + err.Error()})
		return
	}

	version := nextVersion(proto.TargetNode, n.ID)
	recordConfigVersion(proto.TargetNode, n.ID, version, content)

	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    n.AgentID,
		Type:       proto.CmdApply,
		TargetType: proto.TargetNode,
		TargetID:   n.ID,
		Payload:    content,
		Version:    version,
		Flags:      autoStartFlags(n.AutoStart),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if res.Queued {
		c.JSON(http.StatusOK, gin.H{"message": res.Message, "queued": true, "version": version})
		return
	}
	h.hub.SetTargetRunning(n.AgentID, proto.TargetNode, n.ID, res.Running)
	c.JSON(http.StatusOK, gin.H{
		"message":    res.Message,
		"ok":         res.OK,
		"rolledBack": res.RolledBack,
		"version":    version,
		"running":    res.Running,
		"status":     h.statusOf(n),
	})
}

// Start POST /api/nodes/:id/start
func (h *NodeHandler) Start(c *gin.Context) {
	h.control(c, proto.CmdStart)
}

// Stop POST /api/nodes/:id/stop
func (h *NodeHandler) Stop(c *gin.Context) {
	h.control(c, proto.CmdStop)
}

// Restart POST /api/nodes/:id/restart
func (h *NodeHandler) Restart(c *gin.Context) {
	h.control(c, proto.CmdRestart)
}

// Log GET /api/nodes/:id/log
func (h *NodeHandler) Log(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	if n.AgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "节点未绑定 Agent"})
		return
	}
	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    n.AgentID,
		Type:       proto.CmdLog,
		TargetType: proto.TargetNode,
		TargetID:   n.ID,
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
}

func (h *NodeHandler) control(c *gin.Context, cmdType string) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	if n.AgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "节点未绑定 Agent"})
		return
	}
	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    n.AgentID,
		Type:       cmdType,
		TargetType: proto.TargetNode,
		TargetID:   n.ID,
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
	h.hub.SetTargetRunning(n.AgentID, proto.TargetNode, n.ID, res.Running)
	markNodeStopped(n.ID, cmdType == proto.CmdStop)
	c.JSON(http.StatusOK, gin.H{"message": res.Message, "running": res.Running, "status": h.statusOf(n)})
}

// Versions GET /api/nodes/:id/versions 查看托管 Agent 上保存的历史配置版本
func (h *NodeHandler) Versions(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}

	dbList := loadVersionRecords(proto.TargetNode, n.ID)
	if n.AgentID == 0 || !h.hub.Online(n.AgentID) {
		c.JSON(http.StatusOK, gin.H{"fromAgent": false, "versions": dbList, "message": "Agent 离线，仅显示面板记录"})
		return
	}

	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    n.AgentID,
		Type:       proto.CmdVersions,
		TargetType: proto.TargetNode,
		TargetID:   n.ID,
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

// VersionConfig GET /api/nodes/:id/versions/:version/config 读取该节点指定版本的原始配置快照。
// 先查面板留存记录，缺失时再读在线 Agent 的同一目标和版本，不重新生成当前配置。
func (h *NodeHandler) VersionConfig(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本号不合法"})
		return
	}
	record, err := findVersionRecord(proto.TargetNode, n.ID, version)
	if err == nil && record.Content != "" {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"content": record.Content})
		return
	}
	// 面板记录缺失时，尝试从在线 Agent 上仍保留的该版本快照读取。
	if n.AgentID != 0 && h.hub != nil && h.hub.Online(n.AgentID) {
		res, dispatchErr := dispatch(h.hub, &model.AgentCommand{
			AgentID: n.AgentID, Type: proto.CmdVersionConfig,
			TargetType: proto.TargetNode, TargetID: n.ID, Version: version,
			TimeoutMs: 15000,
		})
		if dispatchErr == nil && !res.Queued && res.OK && res.Content != "" {
			c.Header("Cache-Control", "no-store")
			c.JSON(http.StatusOK, gin.H{"content": res.Content})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "该版本的配置快照不存在"})
}

// Rollback POST /api/nodes/:id/rollback 回滚到指定历史版本（由托管 Agent 应用并校验）
func (h *NodeHandler) Rollback(c *gin.Context) {
	var n model.Node
	if err := database.DB.First(&n, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	if n.AgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "节点未绑定 Agent，无法回滚"})
		return
	}

	var req struct {
		Version int `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定要回滚到的版本号"})
		return
	}

	target, err := findVersionRecord(proto.TargetNode, n.ID, req.Version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "面板中未找到该版本的配置记录"})
		return
	}

	version := nextVersion(proto.TargetNode, n.ID)
	recordConfigVersion(proto.TargetNode, n.ID, version, target.Content)

	res, err := dispatch(h.hub, &model.AgentCommand{
		AgentID:    n.AgentID,
		Type:       proto.CmdRollback,
		TargetType: proto.TargetNode,
		TargetID:   n.ID,
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
		"running":    res.Running,
	})
}

// buildConfig 生成节点对应的 frpc.json
func (h *NodeHandler) buildConfig(node *model.Node) (string, error) {
	var server model.FrpsServer
	hasServer := false
	if node.ServerID > 0 {
		if err := database.DB.First(&server, node.ServerID).Error; err == nil {
			hasServer = true
		}
	}
	var proxies []model.Proxy
	database.DB.Where("node_id = ?", node.ID).Find(&proxies)
	var visitors []model.Visitor
	database.DB.Where("node_id = ?", node.ID).Find(&visitors)

	if !hasServer {
		return frp.BuildFrpcFromNode(node, nil, proxies, visitors, h.publicAddr())
	}
	return frp.BuildFrpcFromNode(node, &server, proxies, visitors, h.publicAddr())
}

func (h *NodeHandler) publicAddr() string {
	if h.cfg != nil {
		return h.cfg.PublicURL
	}
	return ""
}

// statusOf 结合 Agent 心跳缓存判断节点运行状态
func (h *NodeHandler) statusOf(n model.Node) string {
	if n.AgentID == 0 {
		return "unbound"
	}
	if !h.hub.Online(n.AgentID) {
		return "agent_offline"
	}
	st, ok := h.hub.State(n.AgentID, proto.TargetNode, n.ID)
	if !ok {
		return n.Status
	}
	if st.Running {
		return "running"
	}
	return "stopped"
}
