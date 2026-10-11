// Package sys 注册系统状态路由.
package sys

import (
	"net/http"

	"github.com/fufuok/pkg/web/gin/middleware"
	"github.com/gin-gonic/gin"
)

// SetupRouter 注册系统路由.
func SetupRouter(app *gin.Engine) {
	sys := app.Group("/sys", middleware.RecoveryWithLog(true))
	{
		sys.GET("/check", func(c *gin.Context) {
			c.String(http.StatusOK, c.ClientIP())
		})
		sys.GET("/stats/*path", runtimeStatsHandler)
	}
}
