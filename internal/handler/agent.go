package handler

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"regexp"
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
	ReadBufferSize:   4096,
	WriteBufferSize:  4096,
	HandshakeTimeout: 10 * time.Second,
	// Agent 可能来自任意来源，且鉴权走签名，不依赖 Origin
	CheckOrigin: func(r *http.Request) bool { return true },
}

// agentAuth 仅接受绑定完整请求、身份及一次性随机数的 v2 签名。
func (h *AgentHandler) agentAuth(c *gin.Context) (*model.Agent, bool) {
	nodeKey := c.GetHeader("X-Node-Key")
	if nodeKey == "" {
		c.JSON(401, gin.H{"error": "缺少 v2 鉴权参数，请更新 Agent"})
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		c.JSON(413, gin.H{"error": "请求正文过大或无法读取"})
		return nil, false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var agent model.Agent
	if err := database.DB.Where("node_key = ?", nodeKey).First(&agent).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未知的安装令牌"})
		return nil, false
	}
	if err := proto.VerifyRequest(c.Request, agent.Secret, body); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return nil, false
	}
	if !consumeNonce(agent.NodeKey + ":" + c.GetHeader("X-Node-Nonce")) {
		c.JSON(401, gin.H{"error": "请求已使用或重放保护容量已满"})
		return nil, false
	}
	if c.GetHeader("X-Agent-Role") != agent.Roles {
		c.JSON(http.StatusConflict, gin.H{"error": "安装命令中的 Agent 类型与面板记录不一致"})
		return nil, false
	}
	instanceID := c.GetHeader("X-Agent-Instance")
	if !instanceIDPattern.MatchString(instanceID) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少有效的 Agent 安装实例标识，请更新 Agent"})
		return nil, false
	}
	hostID := c.GetHeader("X-Agent-Host")
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
	agentIdentityMu.Lock()
	defer agentIdentityMu.Unlock()
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
	agentIdentityMu.Lock()
	defer agentIdentityMu.Unlock()
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}
	configs := make([]proto.ManagedConfig, 0)
	appendLatest := func(targetType string, id uint, autoStart, manualStopped bool) error {
		// 删除/解绑期间不能把刚清理的旧配置恢复回去；后续应用命令才重新授权恢复。
		var latest model.AgentCommand
		err := database.DB.Where("agent_id = ? AND target_type = ? AND target_id = ? AND type IN ?", agent.ID, targetType, id, []string{proto.CmdUnassign, proto.CmdApply, proto.CmdRollback}).Order("id desc").First(&latest).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && latest.Type == proto.CmdUnassign && latest.Status != "failed" {
			return nil
		}
		var record model.ConfigVersion
		err = database.DB.Where("target_type = ? AND target_id = ? AND status IN ? AND content <> ?",
			targetType, id, []string{"applied", "unverified"}, "").Order("version desc").First(&record).Error
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
	agentIdentityMu.Lock()
	defer agentIdentityMu.Unlock()
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
	agentIdentityMu.Lock()
	defer agentIdentityMu.Unlock()
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	cmd, err := claimCommand(agent.ID, 0)
	if err != nil {
		c.JSON(500, gin.H{"error": "查询指令失败"})
		return
	}
	items := make([]proto.CommandData, 0, 1)
	if cmd != nil {
		items = append(items, commandData(cmd))
	}
	c.JSON(200, gin.H{"commands": items, "heartbeatInterval": 30, "serverTime": time.Now().Unix()})

}

// Report POST /api/agent/report 上报指令执行结果（含应用失败与回滚信息）
func (h *AgentHandler) Report(c *gin.Context) {
	agentIdentityMu.Lock()
	defer agentIdentityMu.Unlock()
	agent, ok := h.agentAuth(c)
	if !ok {
		return
	}

	var req struct {
		CommandID uint `json:"commandId"`
		proto.ResultData
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

	if cmd.Status == "pending" {
		c.JSON(409, gin.H{"error": "指令尚未下发"})
		return
	}
	if err := finishCommand(h.hub, &cmd, req.ResultData); err != nil {
		c.JSON(500, gin.H{"error": "记录结果失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已记录"})
}

// WS GET /api/agent/ws 长连接：Agent 主动反连，面板借此实时下发指令
func (h *AgentHandler) WS(c *gin.Context) {
	agentIdentityMu.Lock()
	agent, ok := h.agentAuth(c)
	if !ok {
		agentIdentityMu.Unlock()
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		agentIdentityMu.Unlock()
		return
	}
	conn := h.hub.Attach(agent.ID, ws)
	agentIdentityMu.Unlock()
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
