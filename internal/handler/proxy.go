package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/database"
	"dfpanel/internal/frp"
	"dfpanel/internal/model"
)

// ProxyHandler 隧道（frpc 代理）管理：归属于某个节点，节点配置下发时一并生效
type ProxyHandler struct{}

// NewProxyHandler 创建隧道处理器
func NewProxyHandler() *ProxyHandler {
	return &ProxyHandler{}
}

// List GET /api/nodes/:id/proxies
func (h *ProxyHandler) List(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var list []model.Proxy
	database.DB.Where("node_id = ?", nodeID).Order("id asc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// Create POST /api/nodes/:id/proxies
func (h *ProxyHandler) Create(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var node model.Node
	if err := database.DB.First(&node, nodeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}

	var p model.Proxy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	if err := validateProxy(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p.ID = 0
	p.NodeID = uint(nodeID)

	// 同一节点内名称唯一，frp 以 name 作为代理标识
	var count int64
	database.DB.Model(&model.Proxy{}).Where("node_id = ? AND name = ?", nodeID, p.Name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该节点下已存在同名隧道：" + p.Name})
		return
	}

	if err := database.DB.Create(&p).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

// Update PUT /api/proxies/:id
func (h *ProxyHandler) Update(c *gin.Context) {
	var old model.Proxy
	if err := database.DB.First(&old, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "隧道不存在"})
		return
	}
	var req model.Proxy
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	if err := validateProxy(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.ID = old.ID
	req.NodeID = old.NodeID
	req.CreatedAt = old.CreatedAt
	req.Name = old.Name // 名称作为代理标识，不允许改名

	if err := database.DB.Save(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, req)
}

// Delete DELETE /api/proxies/:id
func (h *ProxyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	if err := database.DB.Delete(&model.Proxy{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// validateProxy 按隧道类型校验必填字段，并补默认值 / 归一化取值
//
// 注意：这里只管「面板侧」的取值合法性，字段该落在 frp 的哪一层由
// internal/frp/frpc_build.go 负责（写错层级会被 frp 判为未知字段）。
func validateProxy(p *model.Proxy) error {
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	if p.Type == "" {
		p.Type = "tcp"
	}
	if strings.TrimSpace(p.Name) == "" {
		return errString("隧道名称必填")
	}
	if p.LocalIP == "" {
		p.LocalIP = "127.0.0.1"
	}
	if p.LocalPort <= 0 || p.LocalPort > 65535 {
		return errString("本地端口必须在 1-65535 之间")
	}

	switch p.Type {
	case "tcp", "udp":
		if p.RemotePort <= 0 || p.RemotePort > 65535 {
			return errString("tcp / udp 隧道必须填写远端端口")
		}
	case "http", "https":
		if strings.TrimSpace(p.CustomDomains) == "" && strings.TrimSpace(p.Subdomain) == "" {
			return errString("http / https 隧道必须填写自定义域名或子域名")
		}
	case "tcpmux":
		if strings.TrimSpace(p.CustomDomains) == "" {
			return errString("tcpmux 隧道必须填写自定义域名（作为 httpconnect 的路由标识）")
		}
		if p.Multiplexer == "" {
			p.Multiplexer = "httpconnect"
		}
		if p.Multiplexer != "httpconnect" {
			return errString("tcpmux 的复用器目前只支持 httpconnect")
		}
	case "stcp", "sudp", "xtcp":
		// 点对点隧道靠 serverName + secretKey 配对，与 loadBalancer 分组无关；
		// secretKey 在 frp 里可留空，但留空时同用户的任何访问端都能连，界面上给出提示即可，
		// 这里不做强制，避免存量数据无法保存。
	default:
		return errString("不支持的隧道类型：" + p.Type)
	}

	// PROXY protocol 只允许 v1 / v2（取值不对会让 frpc 拒绝加载）
	if v := strings.TrimSpace(p.ProxyProtocolVersion); v != "" {
		v = strings.ToLower(v)
		if v != "v1" && v != "v2" {
			return errString("PROXY protocol 版本只能是 v1 或 v2")
		}
		p.ProxyProtocolVersion = v
	}
	if v := strings.TrimSpace(p.BandwidthLimitMode); v != "" {
		v = strings.ToLower(v)
		if v != "client" && v != "server" {
			return errString("限速位置只能是 client 或 server")
		}
		p.BandwidthLimitMode = v
	}
	if v := strings.TrimSpace(p.HealthCheckType); v != "" {
		v = strings.ToLower(v)
		if v != "tcp" && v != "http" {
			return errString("健康检查类型只能是 tcp 或 http")
		}
		p.HealthCheckType = v
		if v == "http" && strings.TrimSpace(p.HealthCheckPath) == "" {
			p.HealthCheckPath = "/"
		}
	}

	// 插件参数是 JSON 对象，提前校验，避免坏配置走到下发阶段才被 frpc 拒绝
	if strings.TrimSpace(p.PluginType) != "" {
		if _, err := frp.ParseJSONObject(p.PluginConfig); err != nil {
			return errString(err.Error())
		}
	} else if strings.TrimSpace(p.PluginConfig) != "" {
		return errString("填写了插件参数但未选择插件类型")
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }
