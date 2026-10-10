package middleware

import (
	"crypto/rand"
	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// GenerateToken 在调用方的事务中保存会话，使密码校验与会话签发保持一致。
func GenerateToken(db *gorm.DB, secret string, expireHours int, uid uint, username string) (string, error) {
	if expireHours <= 0 {
		expireHours = 24
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	expires := time.Now().Add(time.Duration(expireHours) * time.Hour)
	claims := Claims{UserID: uid, Username: username, RegisteredClaims: jwt.RegisteredClaims{ID: id, ExpiresAt: jwt.NewNumericDate(expires), IssuedAt: jwt.NewNumericDate(time.Now()), Issuer: "dfpanel"}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", err
	}
	if err := db.Create(&model.AuthSession{ID: id, UserID: uid, ExpiresAt: expires}).Error; err != nil {
		return "", err
	}
	db.Where("expires_at < ?", time.Now()).Delete(&model.AuthSession{})
	return token, nil
}
func JWT(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			c.AbortWithStatusJSON(401, gin.H{"error": "未登录"})
			return
		}
		claims := &Claims{}
		_, err := jwt.ParseWithClaims(strings.TrimSpace(strings.TrimPrefix(raw, "Bearer ")), claims, func(t *jwt.Token) (interface{}, error) { return []byte(secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("dfpanel"), jwt.WithExpirationRequired())
		var count int64
		if err != nil || claims.ID == "" || claims.UserID == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录"})
			return
		}
		err = database.DB.Model(&model.AuthSession{}).Where("id = ? AND user_id = ? AND expires_at > ?", claims.ID, claims.UserID, time.Now()).Count(&count).Error
		var user model.User
		if err != nil || count != 1 || database.DB.First(&user, claims.UserID).Error != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "登录已失效，请重新登录"})
			return
		}
		c.Set("uid", user.ID)
		c.Set("username", user.Username)
		c.Set("sessionId", claims.ID)
		c.Next()
	}
}
