package engine

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// SetupSYSRouter 设置系统信息路由
func SetupSYSRouter(app *gin.Engine) {
	app.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "PONG")
	})
	app.GET("/client_ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
}

// SetupExceptionRouter 设置异常请求路由
func SetupExceptionRouter(app *gin.Engine) {
	app.NoRoute(func(c *gin.Context) {
		c.String(http.StatusNotFound, "404")
	})
	app.NoMethod(func(c *gin.Context) {
		c.String(http.StatusMethodNotAllowed, "405")
	})
}
