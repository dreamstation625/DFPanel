package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
)

// VisitorHandler 访问端（frpc 的 visitors）管理：归属于某个节点，节点配置下发时一并生效
//
// 点对点隧道（stcp / sudp / xtcp）需要两侧配合：服务侧配 tunnel（proxies），
// 访问侧配 visitor，缺一不可。
type VisitorHandler struct{}

// NewVisitorHandler 创建访问端处理器
func NewVisitorHandler() *VisitorHandler {
	return &VisitorHandler{}
}

// List GET /api/nodes/:id/visitors
func (h *VisitorHandler) List(c *gin.Context) {
	nodeID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	var list []model.Visitor
	database.DB.Where("node_id = ?", nodeID).Order("id asc").Find(&list)
	c.JSON(http.StatusOK, list)
}

// Create POST /api/nodes/:id/visitors
func (h *VisitorHandler) Create(c *gin.Context) {
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

	var v model.Visitor
	if err := c.ShouldBindJSON(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	if err := validateVisitor(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	v.ID = 0
	v.NodeID = uint(nodeID)

	var count int64
	database.DB.Model(&model.Visitor{}).Where("node_id = ? AND name = ?", nodeID, v.Name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该节点下已存在同名访问端：" + v.Name})
		return
	}
	if err := database.DB.Create(&v).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

// Update POST /api/visitors/:id/update
func (h *VisitorHandler) Update(c *gin.Context) {
	var old model.Visitor
	if err := database.DB.First(&old, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "访问端不存在"})
		return
	}
	var req model.Visitor
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}
	if err := validateVisitor(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.ID = old.ID
	req.NodeID = old.NodeID
	req.CreatedAt = old.CreatedAt
	req.Name = old.Name // 名称作为 frp 的 visitor 标识，不允许改名

	if err := database.DB.Save(&req).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, req)
}

// Delete POST /api/visitors/:id/delete
func (h *VisitorHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}
	if err := database.DB.Delete(&model.Visitor{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// validateVisitor 校验访问端配置
func validateVisitor(v *model.Visitor) error {
	v.Type = strings.ToLower(strings.TrimSpace(v.Type))
	if v.Name = strings.TrimSpace(v.Name); v.Name == "" {
		return errString("访问端名称必填")
	}
	switch v.Type {
	case "stcp", "sudp", "xtcp":
	case "":
		return errString("请选择访问端类型")
	default:
		return errString("访问端类型只能是 stcp / sudp / xtcp")
	}
	v.ServerName = strings.TrimSpace(v.ServerName)
	if v.ServerName == "" {
		return errString("要访问的服务端代理名必填（对应隧道名称）")
	}

	// bindPort 允许为负数：表示不监听端口，只接受其它 visitor 转发的连接
	if v.BindPort > 65535 {
		return errString("本地绑定端口不能超过 65535")
	}
	if v.Type == "sudp" && v.BindPort < 0 {
		return errString("sudp 访问端不支持 bindPort 为负（只接收转发连接）")
	}
	if v.BindAddr == "" {
		v.BindAddr = "127.0.0.1"
	}
	return nil
}
