package handler

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

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

var errPasswordChanged = errors.New("密码已修改，请重新登录")
var errPasswordSessionExpired = errors.New("登录已失效，请重新登录")

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

	key := "login:" + c.ClientIP()
	if !allowAuthAttempt(c, key) {
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

	token, err := h.createLoginSession(user)
	if errors.Is(err, errPasswordChanged) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}
	clearAuthAttempts(key)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  gin.H{"id": user.ID, "username": user.Username, "role": user.Role},
	})
}

// allowAuthAttempt 登录按 IP 限速，修改密码按账号限速；两种操作的计数互不影响。
func allowAuthAttempt(c *gin.Context, key string) bool {
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
		return false
	}
	if len(loginAttempts) >= 10000 {
		loginMu.Unlock()
		c.JSON(429, gin.H{"error": "尝试过多，请稍后重试"})
		return false
	}
	attempt.Count++
	attempt.Until = now.Add(15 * time.Minute)
	loginAttempts[key] = attempt
	loginMu.Unlock()
	return true
}

func clearAuthAttempts(key string) {
	loginMu.Lock()
	delete(loginAttempts, key)
	loginMu.Unlock()
}

func (h *AuthHandler) createLoginSession(user model.User) (string, error) {
	var token string
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.First(&current, user.ID).Error; err != nil {
			return err
		}
		// bcrypt 校验在事务外完成；重新核对摘要，防止改密码后旧凭据签发新会话。
		if current.PasswordHash != user.PasswordHash {
			return errPasswordChanged
		}
		var err error
		token, err = middleware.GenerateToken(tx, h.cfg.JWTSecret, h.cfg.TokenExpireHrs, current.ID, current.Username)
		return err
	})
	return token, err
}

type changePasswordReq struct {
	CurrentPassword string `json:"currentPassword" binding:"required,max=64"`
	NewPassword     string `json:"newPassword" binding:"required,min=6,max=64"`
}

// ChangePassword POST /api/auth/password 校验当前密码，更新管理员密码并撤销全部会话。
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写当前密码，新密码应为 6–64 个字符"})
		return
	}
	// bcrypt 最多接受 72 字节；多字节字符仍须满足这个限制。
	if len(req.NewPassword) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能超过 72 字节，请减少中文或其他多字节字符"})
		return
	}
	var user model.User
	if err := database.DB.First(&user, c.GetUint("uid")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取管理员账号失败"})
		return
	}
	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅管理员可修改密码"})
		return
	}
	key := "password:" + strconv.FormatUint(uint64(user.ID), 10)
	if !allowAuthAttempt(c, key) {
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		// 当前密码填错不代表登录会话失效，返回 400 让用户留在表单中重试。
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前密码不正确"})
		return
	}
	if req.NewPassword == req.CurrentPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "新密码不能与当前密码相同"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.AuthSession{}).Where("id = ? AND user_id = ? AND expires_at > ?", c.GetString("sessionId"), user.ID, time.Now()).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errPasswordSessionExpired
		}
		// 条件更新拒绝并发覆盖；撤销会话失败时，密码修改也一并回滚。
		result := tx.Model(&model.User{}).Where("id = ? AND password_hash = ? AND role = ?", user.ID, user.PasswordHash, "admin").Update("password_hash", string(hash))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errPasswordChanged
		}
		return tx.Where("user_id = ?", user.ID).Delete(&model.AuthSession{}).Error
	})
	if errors.Is(err, errPasswordSessionExpired) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, errPasswordChanged) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "修改密码失败，请稍后重试"})
		return
	}
	clearAuthAttempts(key)
	c.JSON(http.StatusOK, gin.H{"message": "密码已修改，请重新登录"})
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
