package handler

import (
	"errors"
	"gorm.io/gorm"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/middleware"
	"dfpanel/internal/model"
)

// AuthHandler 认证相关接口
var initMu sync.Mutex
var loginMu sync.Mutex

type loginAttempt struct {
	Count int
	Until time.Time
}

var loginAttempts = map[string]loginAttempt{}

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
	if err := database.DB.Model(&model.User{}).Count(&count).Error; err != nil {
		c.JSON(500, gin.H{"error": "读取初始化状态失败"})
		return
	}
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
	initMu.Lock()
	defer initMu.Unlock()
	var count int64
	if err := database.DB.Model(&model.User{}).Count(&count).Error; err != nil {
		c.JSON(500, gin.H{"error": "读取初始化状态失败"})
		return
	}
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
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("系统已初始化")
		}
		if err := tx.Create(&model.Setting{Key: "admin_initialized", Value: "1"}).Error; err != nil {
			return err
		}
		return tx.Create(&user).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建管理员失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "初始化成功"})
}

type loginReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
}

// Login POST /api/auth/login 登录
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数不合法"})
		return
	}

	key := c.ClientIP()
	loginMu.Lock()
	now := time.Now()
	for k, v := range loginAttempts {
		if now.After(v.Until) {
			delete(loginAttempts, k)
		}
	}
	attempt := loginAttempts[key]
	if attempt.Count >= 10 && now.Before(attempt.Until) {
		loginMu.Unlock()
		c.Header("Retry-After", "900")
		c.JSON(429, gin.H{"error": "尝试过多，请稍后重试"})
		return
	}
	if len(loginAttempts) >= 10000 {
		loginMu.Unlock()
		c.JSON(429, gin.H{"error": "尝试过多，请稍后重试"})
		return
	}
	attempt.Count++
	attempt.Until = now.Add(15 * time.Minute)
	loginAttempts[key] = attempt
	loginMu.Unlock()
	var user model.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	loginMu.Lock()
	delete(loginAttempts, key)
	loginMu.Unlock()
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

// Logout 撤销当前会话或管理员的全部会话。
func (h *AuthHandler) Logout(c *gin.Context) {
	q := database.DB.Where("id = ? AND user_id = ?", c.GetString("sessionId"), c.GetUint("uid"))
	if c.Query("all") == "true" {
		q = database.DB.Where("user_id = ?", c.GetUint("uid"))
	}
	if err := q.Delete(&model.AuthSession{}).Error; err != nil {
		c.JSON(500, gin.H{"error": "注销失败"})
		return
	}
	c.JSON(200, gin.H{"message": "已注销"})
}
