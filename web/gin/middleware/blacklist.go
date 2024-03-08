package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *gin.Context) {
		if BlacklistChecker(c) {
			responseForbidden(c, errMsg, asAPI)
			return
		}
		c.Next()
	}
}

// BlacklistChecker 是否存在于黑名单, true 是黑名单 (黑名单为空时: 放过, false)
func BlacklistChecker(c *gin.Context) bool {
	clientIP := c.ClientIP()
	if len(config.Blacklist) > 0 {
		_, ok := common.LookupIPNetsString(clientIP, config.Blacklist)
		return ok
	}
	return false
}
