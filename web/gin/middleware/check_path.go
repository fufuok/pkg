package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
)

// CheckPathOr404 检查 URL 是否为配置中的路由
func CheckPathOr404() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 注: StatsPath 设定值必须以 / 开头
		v := c.Param("path")
		if v == "" || v != config.Config().WebConf.StatsPath {
			c.Abort()
			c.String(http.StatusNotFound, "404")
			return
		}

		c.Next()
	}
}
