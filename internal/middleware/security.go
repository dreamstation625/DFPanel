package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

// Security 为接口统一限制输入大小、禁止缓存机密并校验路径主键。
func Security() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
		}
		c.Header("X-Content-Type-Options", "nosniff")
		for _, key := range []string{"id", "version"} {
			if raw := c.Param(key); raw != "" && strings.HasPrefix(c.Request.URL.Path, "/api/") {
				n, err := strconv.ParseUint(raw, 10, 32)
				if err != nil || n == 0 || strconv.FormatUint(n, 10) != raw {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "编号必须是正整数"})
					return
				}
			}
		}
		c.Next()
	}
}
