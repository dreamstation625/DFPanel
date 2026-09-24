package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/database"
	"dfpanel/internal/distrib"
	"dfpanel/internal/model"
)

// AgentManageHandler Agent 管理面接口（JWT 鉴权）
type AgentManageHandler struct {
	hub *agenthub.Hub
}

// NewAgentManageHandler 创建 Agent 管理处理器
func NewAgentManageHandler(hub *agenthub.Hub) *AgentManageHandler {
	return &AgentManageHandler{hub: hub}
}

// List GET /api/agents
func (h *AgentManageHandler) List(c *gin.Context) {
	var list []model.Agent
	database.DB.Order("id asc").Find(&list)

	type item struct {
		model.Agent
		Online bool `json:"online"`
	}
	out := make([]item, 0, len(list))
	for _, a := range list {
		out = append(out, item{Agent: a, Online: h.hub.Online(a.ID)})
	}
	c.JSON(http.StatusOK, out)
}

// Get GET /api/agents/:id
func (h *AgentManageHandler) Get(c *gin.Context) {
	var a model.Agent
	if err := database.DB.First(&a, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"agent": a, "online": h.hub.Online(a.ID)})
}

type createAgentReq struct {
	Name       string `json:"name"`
	Remark     string `json:"remark"`
	Roles      string `json:"roles"`      // frps / frpc / frps,frpc
	FrpVersion string `json:"frpVersion"` // 选填：创建后先把该版本的 frp 二进制预置到目标机器，不切换、不重启
}

// Create POST /api/agents 创建 Agent 并生成安装令牌
func (h *AgentManageHandler) Create(c *gin.Context) {
	var req createAgentReq
	_ = c.ShouldBindJSON(&req)

	if req.Roles == "" {
		req.Roles = "frpc"
	}
	if req.Name == "" {
		req.Name = "Agent-" + time.Now().Format("20060102-150405")
	}

	// 版本号在面板侧解析好再下发，避免各 Agent 自己解释 latest 造成版本漂移
	version := strings.TrimSpace(req.FrpVersion)
	if version == distrib.LatestTag {
		resolved, err := distrib.LatestVersion()
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "解析最新 frp 版本失败：" + err.Error()})
			return
		}
		version = resolved
	}
	if version != "" && !distrib.ValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "frp 版本号不合法：" + req.FrpVersion})
		return
	}

	a := model.Agent{
		Name:       req.Name,
		Remark:     req.Remark,
		Roles:      req.Roles,
		FRPVersion: version,
		// NodeKey / Secret 只在创建与安装命令中返回，后续不再展示 Secret
		NodeKey: randomToken(16),
		Secret:  randomToken(32),
		Status:  "offline",
	}
	if err := database.DB.Create(&a).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败：" + err.Error()})
		return
	}

	// 期望版本只落在记录里：下载统一在设置页做，切换由用户在 Agent 上点。
	// 这里不下发任何指令，免得 Agent 刚上线就被拖去抓上游。
	c.JSON(http.StatusOK, a)
}

// Update PUT /api/agents/:id
func (h *AgentManageHandler) Update(c *gin.Context) {
	var old model.Agent
	if err := database.DB.First(&old, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}
	var req struct {
		Name   string `json:"name"`
		Remark string `json:"remark"`
		Roles  string `json:"roles"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}

	// 编辑表单只包含这三个字段；其他状态由注册、心跳和版本操作维护。
	// 整行 Save 会把未提交的 frp 实际版本、期望版本及运行时清空。
	if err := database.DB.Model(&old).Updates(map[string]any{
		"name":   req.Name,
		"remark": req.Remark,
		"roles":  req.Roles,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	if err := database.DB.First(&old, old.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取保存结果失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, old)
}

// Delete DELETE /api/agents/:id
func (h *AgentManageHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	if err := database.DB.Delete(&model.Agent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ResetToken POST /api/agents/:id/token/reset 重置安装密钥（重置后旧客户端需重新安装）
func (h *AgentManageHandler) ResetToken(c *gin.Context) {
	var a model.Agent
	if err := database.DB.First(&a, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}
	key := randomToken(16)
	secret := randomToken(32)
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", a.ID).
		Updates(map[string]any{"node_key": key, "secret": secret, "status": "offline"}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置失败：" + err.Error()})
		return
	}
	a.NodeKey = key
	a.Secret = secret
	a.Status = "offline"
	c.JSON(http.StatusOK, a)
}

// Commands GET /api/agents/:id/commands 查看指令历史（排障用）
func (h *AgentManageHandler) Commands(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var list []model.AgentCommand
	database.DB.Where("agent_id = ?", id).Order("id desc").Limit(50).Find(&list)
	for i := range list {
		// 配置全文不回传，避免响应过大
		if len(list[i].Payload) > 0 {
			list[i].Payload = ""
		}
	}
	c.JSON(http.StatusOK, list)
}

// Versions GET /api/config-versions?targetType=server&targetId=1 配置版本历史（可据此回滚）
func (h *AgentManageHandler) Versions(c *gin.Context) {
	targetType := c.Query("targetType")
	targetID, _ := strconv.ParseUint(c.Query("targetId"), 10, 64)
	if targetType == "" || targetID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "targetType / targetId 必填"})
		return
	}

	type versionItem struct {
		ID        uint      `json:"id"`
		Version   int       `json:"version"`
		Status    string    `json:"status"`
		Message   string    `json:"message"`
		Checksum  string    `json:"checksum"`
		CreatedAt time.Time `json:"createdAt"`
	}
	var list []versionItem
	database.DB.Model(&model.ConfigVersion{}).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("version desc").Limit(50).
		Select("id", "version", "status", "message", "checksum", "created_at").
		Find(&list)
	c.JSON(http.StatusOK, list)
}
