// Package api 注册业务路由.
package api

import "github.com/gin-gonic/gin"

// SetupRouter 注册业务接口.
func SetupRouter(app *gin.Engine) {
	v1 := app.Group("/v1")
	{
		v1.GET("/status", statusHandler)
		v1.POST("/echo", echoHandler)
		v1.POST("/open", openHandler)
	}
}
