package handler

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

var instanceIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// AgentHandler Agent 面接口：注册、心跳、指令拉取、结果上报与 WebSocket 长连接
type AgentHandler struct {
	hub *agenthub.Hub
}

// NewAgentHandler 创建 Agent 处理器
func NewAgentHandler(hub *agenthub.Hub) *AgentHandler {
	return &AgentHandler{hub: hub}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Agent 可能来自任意来源，且鉴权走签名，不依赖 Origin
	CheckOrigin: func(r *http.Request) bool { return true },
}

// agentAuth 校验 Agent 签名：sign = HMAC-SHA256(secret, "nodeKey.ts")
func (h *AgentHandler) agentAuth(c *gin.Context) (*model.Agent, bool) {
	nodeKey := firstNonEmpty(c.Query("nodeKey"), c.GetHeader("X-Node-Key"))
	sign := firstNonEmpty(c.Query("sign"), c.GetHeader("X-Node-Sign"))
	tsStr := firstNonEmpty(c.Query("ts"), c.GetHeader("X-Node-Ts"))

	if nodeKey == "" || sign == "" || tsStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少鉴权参数"})
		return nil, false
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "时间戳不合法"})
		return nil, false
	}

	var agent model.Agent
	if err := database.DB.Where("node_key = ?", nodeKey).First(&agent).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未知的安装令牌"})
		return nil, false
	}
	if err := proto.Verify(agent.Secret, agent.NodeKey, ts, sign, 0); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return nil, false
	}
	if c.Query("role") != agent.Roles {
		c.JSON(http.StatusConflict, gin.H{"error": "安装命令中的 Agent 类型与面板记录不一致"})
		return nil, false
	}
	instanceID := firstNonEmpty(c.Query("instanceId"), c.GetHeader("X-Agent-Instance"))
	if !instanceIDPattern.MatchString(instanceID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少有效的 Agent 安装实例标识，请更新 Agent"})
		return nil, false
	}
	hostID := firstNonEmpty(c.Query("hostId"), c.GetHeader("X-Agent-Host"))
	if !instanceIDPattern.MatchString(hostID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少有效的 Agent 宿主标识，请更新 Agent"})
		return nil, false
	}
	if agent.InstanceID != "" && agent.InstanceID != instanceID {
		c.JSON(http.StatusConflict, gin.H{"error": "该 Agent 身份已被另一安装实例使用；请为这台客户端创建独立 Agent"})
		return nil, false
	}
	if agent.HostID != "" && agent.HostID != hostID {
		c.JSON(http.StatusConflict, gin.H{"error": "该 Agent 身份已绑定另一台宿主机；请创建独立 Agent"})
		return nil, false
	}
	if agent.InstanceID == "" || agent.HostID == "" {
		var existing int64
		if err := database.DB.Model(&model.Agent{}).Where("instance_id = ? AND id <> ?", instanceID, agent.ID).Count(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "检查安装实例失败"})
			return nil, false
		}
		if existing > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "该安装实例已绑定另一 Agent，请为本机新建独立安装目录"})
			return nil, false
		}
		if err := database.DB.Model(&model.Agent{}).
			Where("id = ? AND instance_id = ? AND host_id = ?", agent.ID, agent.InstanceID, agent.HostID).
			Updates(map[string]any{"instance_id": instanceID, "host_id": hostID}).Error; err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "绑定安装实例失败；该实例可能已被另一 Agent 使用"})
			return nil, false
		}
		if err := database.DB.Select("instance_id", "host_id").First(&agent, agent.ID).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取安装实例失败"})
			return nil, false
		}
	}
	if agent.InstanceID != instanceID || agent.HostID != hostID {
		c.JSON(http.StatusConflict, gin.H{"error": "该 Agent 身份已绑定其它安装实例或宿主机"})
		return nil, false
	}
	return &agent, true
}

// Register POST /api/agent/register 注册并上报机器信息
func (h *AgentHandler) Register(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	var req struct {
		Version  string `json:"version"`
		Hostname string `json:"hostname"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Roles    string `json:"roles"`
		Runtime  string `json:"runtime"`
		HostID   string `json:"hostId"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.Roles != "" && req.Roles != agent.Roles {
		c.JSON(http.StatusConflict, gin.H{"error": "安装命令中的 Agent 类型与面板记录不一致"})
		return
	}

	updates := map[string]any{
		"status":      "online",
		"last_seen":   time.Now(),
		"remote_addr": c.ClientIP(),
		"last_error":  "",
	}
	if req.Version != "" {
		updates["version"] = req.Version
	}
	if req.Hostname != "" {
		updates["hostname"] = req.Hostname
	}
	if req.OS != "" {
		updates["os"] = req.OS
	}
	if req.Arch != "" {
		updates["arch"] = req.Arch
	}
	if req.HostID != "" && req.HostID != agent.HostID {
		c.JSON(http.StatusConflict, gin.H{"error": "上报的宿主标识与安装实例不一致"})
		return
	}
	if req.Runtime != "" {
		updates["runtime"] = req.Runtime
	}
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败：" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"agentId":           agent.ID,
		"name":              agent.Name,
		"roles":             agent.Roles,
		"heartbeatInterval": 30,
		"serverTime":        time.Now().Unix(),
	})
}

// ManagedConfigs GET /api/agent/configs 仅返回当前 Agent 托管对象的最近成功应用快照。
// 配置含鉴权信息，必须经过 Agent 签名鉴权，且不允许浏览器或代理缓存。
func (h *AgentHandler) ManagedConfigs(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}
	configs := make([]proto.ManagedConfig, 0)
	appendLatest := func(targetType string, id uint, autoStart, manualStopped bool) error {
		var record model.ConfigVersion
		err := database.DB.Where("target_type = ? AND target_id = ? AND status = ? AND content <> ?",
			targetType, id, "applied", "").Order("version desc").First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		configs = append(configs, proto.ManagedConfig{
			TargetType: targetType, TargetID: id, Version: record.Version,
			Content: record.Content, AutoStart: autoStart, ManualStopped: manualStopped,
		})
		return nil
	}
	if agent.HasRole("frpc") {
		var nodes []model.Node
		if err := database.DB.Where("agent_id = ?", agent.ID).Order("id asc").Find(&nodes).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询托管节点失败"})
			return
		}
		for _, n := range nodes {
			if err := appendLatest(proto.TargetNode, n.ID, n.AutoStart, n.ManualStopped); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "查询节点配置快照失败"})
				return
			}
		}
	}
	if agent.HasRole("frps") {
		var servers []model.FrpsServer
		if err := database.DB.Where("agent_id = ? AND deploy_mode = ?", agent.ID, "agent").
			Order("id asc").Find(&servers).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询托管服务端失败"})
			return
		}
		for _, s := range servers {
			if err := appendLatest(proto.TargetServer, s.ID, s.AutoStart, s.ManualStopped); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "查询服务端配置快照失败"})
				return
			}
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"configs": configs})
}

// Heartbeat POST /api/agent/heartbeat 状态上报（WebSocket 不可用时的降级通道）
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}
	var hb proto.HeartbeatData
	if err := c.ShouldBindJSON(&hb); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	h.hub.UpdateState(agent.ID, hb, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"serverTime": time.Now().Unix()})
}

// Commands GET /api/agent/commands 拉取待执行指令（离线补发 / 轮询降级）
func (h *AgentHandler) Commands(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	var list []model.AgentCommand
	// 清理指令可重复执行；若 Agent 在领取后崩溃，超时后重新投递，避免旧实例永久残留。
	staleCleanupBefore := time.Now().Add(-2 * time.Minute)
	if err := database.DB.Where("agent_id = ? AND (status = ? OR (type = ? AND status = ? AND sent_at < ?))",
		agent.ID, "pending", proto.CmdUnassign, "sent", staleCleanupBefore).
		Order("id asc").Limit(20).Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询指令失败"})
		return
	}

	now := time.Now()
	items := make([]proto.CommandData, 0, len(list))
	for _, cmd := range list {
		items = append(items, proto.CommandData{
			CommandID:  cmd.ID,
			Type:       cmd.Type,
			TargetType: cmd.TargetType,
			TargetID:   cmd.TargetID,
			Payload:    cmd.Payload,
			Version:    cmd.Version,
			Flags:      cmd.Flags,
		})
		if err := database.DB.Model(&model.AgentCommand{}).Where("id = ?", cmd.ID).
			Updates(map[string]any{"status": "sent", "sent_at": now}).Error; err != nil {
			continue
		}
	}

	c.JSON(http.StatusOK, gin.H{"commands": items, "heartbeatInterval": 30, "serverTime": now.Unix()})
}

// Report POST /api/agent/report 上报指令执行结果（含应用失败与回滚信息）
func (h *AgentHandler) Report(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	var req struct {
		CommandID  uint   `json:"commandId"`
		OK         bool   `json:"ok"`
		Message    string `json:"message"`
		Content    string `json:"content"`
		Running    bool   `json:"running"`
		Version    int    `json:"version"`
		Checksum   string `json:"checksum"`
		RolledBack bool   `json:"rolledBack"`
		Unverified bool   `json:"unverified"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}

	var cmd model.AgentCommand
	if err := database.DB.Where("id = ? AND agent_id = ?", req.CommandID, agent.ID).First(&cmd).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "指令不存在"})
		return
	}

	status := "done"
	if !req.OK {
		status = "failed"
	}
	markCommand(cmd.ID, status, req.Message)
	if req.OK {
		// 离线排队的启停指令通过 HTTP report 完成，面板也要保存手动停止意图，
		// 供 Agent 丢失配置文件后的启动恢复使用。
		switch cmd.Type {
		case proto.CmdStop, proto.CmdStart, proto.CmdRestart:
			switch cmd.TargetType {
			case proto.TargetNode:
				markNodeStopped(cmd.TargetID, cmd.Type == proto.CmdStop)
			case proto.TargetServer:
				markServerStopped(cmd.TargetID, cmd.Type == proto.CmdStop)
			}
		}
	}

	// apply 结果回写配置版本状态，形成可追溯的变更历史
	if cmd.Type == "apply" || cmd.Type == "rollback" {
		st := "applied"
		switch {
		case req.RolledBack:
			st = "rolled_back"
		case !req.OK:
			st = "failed"
		case req.Unverified:
			st = "unverified"
		}
		var cv model.ConfigVersion
		if err := database.DB.Where("target_type = ? AND target_id = ? AND version = ?",
			cmd.TargetType, cmd.TargetID, cmd.Version).First(&cv).Error; err == nil {
			setConfigVersionStatus(cv.ID, st, req.Message)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "已记录"})
}

// WS GET /api/agent/ws 长连接：Agent 主动反连，面板借此实时下发指令
func (h *AgentHandler) WS(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn := h.hub.Attach(agent.ID, ws)
	conn.Wait()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
