package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/middleware"
	"dfpanel/internal/model"
)

// AuthHandler 认证相关接口
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

// InitStatus GET /api/init-status 查询系统是否已初始化
// 同时返回面板版本号，供前端在登录页与侧边栏展示
func (h *AuthHandler) InitStatus(c *gin.Context) {
	var count int64
	database.DB.Model(&model.User{}).Count(&count)
	version := "dev"
	if h.cfg != nil && h.cfg.Version != "" {
		version = h.cfg.Version
	}
	c.JSON(http.StatusOK, gin.H{"initialized": count > 0, "version": version})
}

type initReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=64"`
}

// Init POST /api/init 首次初始化管理员
func (h *AuthHandler) Init(c *gin.Context) {
	var count int64
	database.DB.Model(&model.User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "系统已初始化"})
		return
	}

	var req initReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法：" + err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	user := model.User{Username: req.Username, PasswordHash: string(hash), Role: "admin"}
	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建管理员失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "初始化成功"})
}

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login POST /api/auth/login 登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}

	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	token, err := middleware.GenerateToken(h.cfg.JWTSecret, h.cfg.TokenExpireHrs, user.ID, user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  gin.H{"id": user.ID, "username": user.Username, "role": user.Role},
	})
}

// Profile GET /api/auth/profile 当前用户信息
func (h *AuthHandler) Profile(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":       c.GetUint("uid"),
		"username": c.GetString("username"),
	})
}
