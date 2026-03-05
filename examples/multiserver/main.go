// 演示在同一应用中, 使用不同配置 (Name, 端口) 同时启动多个 Web 服务.
//
// gin-api     :6600, :6602 (多端口)
// gin-admin   :6603
// fiber-web   :6601
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	fiberengine "github.com/fufuok/pkg/web/fiber/engine"
	ginengine "github.com/fufuok/pkg/web/gin/engine"
)

func init() {
	config.Debug = true
}

func main() {
	config.InitTester()
	_ = new(common.M).Start()

	// Gin API 服务, 多端口监听
	ginAPIConf := config.WebConf{
		Name:       "gin-api",
		ServerAddr: ":6600,:6602",
	}

	// Gin Admin 服务, 单独端口
	ginAdminConf := config.WebConf{
		Name:       "gin-admin",
		ServerAddr: ":6603",
	}

	// Fiber Web 服务
	fiberConf := config.WebConf{
		Name:       "fiber-web",
		ServerAddr: ":6601",
	}

	go ginengine.Run(setupGinAPI, ginAPIConf)
	go ginengine.Run(setupGinAdmin, ginAdminConf)
	fiberengine.Run(setupFiber, fiberConf)
}

func setupGinAPI(app *gin.Engine) *gin.Engine {
	app.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "gin-api", "message": "pong"})
	})
	return app
}

func setupGinAdmin(app *gin.Engine) *gin.Engine {
	app.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"service": "gin-admin", "message": "pong"})
	})
	return app
}

func setupFiber(app *fiber.App) *fiber.App {
	app.Get("/ping", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"service": "fiber-web", "message": "pong"})
	})
	return app
}
