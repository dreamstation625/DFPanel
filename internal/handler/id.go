package handler

import (
	"github.com/gin-gonic/gin"
	"strconv"
)

// routeID 仅返回数值主键，生产路由另由 Security 中间件拒绝非法输入。
func routeID(c *gin.Context) uint {
	n, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		return 0
	}
	return uint(n)
}
