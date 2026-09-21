package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

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
	}
	_ = c.ShouldBindJSON(&req)

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
	if req.Roles != "" {
		updates["roles"] = req.Roles
	}
	if req.Runtime != "" {
		updates["runtime"] = req.Runtime
	}
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", agent.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册失败：" + err.Error()})
		return
	}

	if req.Roles != "" {
		agent.Roles = req.Roles
	}
	c.JSON(http.StatusOK, gin.H{
		"agentId":           agent.ID,
		"name":              agent.Name,
		"roles":             agent.Roles,
		"heartbeatInterval": 30,
		"serverTime":        time.Now().Unix(),
	})
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

	// frp 版本与运行时信息随心跳落库，供界面在 Agent 离线时也能展示上次已知状态
	if hb.FrpVersion != "" || len(hb.FrpCached) > 0 || hb.Runtime != "" {
		updates := map[string]any{}
		if hb.Runtime != "" {
			updates["runtime"] = hb.Runtime
		}
		if hb.FrpVersion != "" {
			updates["frp_installed_version"] = hb.FrpVersion
		}
		if len(hb.FrpCached) > 0 {
			updates["frp_cached_versions"] = strings.Join(hb.FrpCached, ",")
		}
		if len(updates) > 0 {
			_ = database.DB.Model(&model.Agent{}).Where("id = ?", agent.ID).Updates(updates).Error
		}
	}
	c.JSON(http.StatusOK, gin.H{"serverTime": time.Now().Unix()})
}

// Commands GET /api/agent/commands 拉取待执行指令（离线补发 / 轮询降级）
func (h *AgentHandler) Commands(c *gin.Context) {
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	var list []model.AgentCommand
	if err := database.DB.Where("agent_id = ? AND status = ?", agent.ID, "pending").
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
