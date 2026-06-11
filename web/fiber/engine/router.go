package engine

import (
	"github.com/gofiber/fiber/v3"
)

// SetupSYSRouter 设置系统信息路由
func SetupSYSRouter(app *fiber.App) {
	app.Get("/ping", func(c fiber.Ctx) error {
		return c.SendString("PONG")
	})
	app.Get("/client_ip", func(c fiber.Ctx) error {
		return c.SendString(c.IP())
	})
}

// SetupExceptionRouter 设置异常请求路由.
//
// Fiber 没有与 Gin NoRoute/NoMethod 完全一致的入口, 这里必须在全部业务路由注册完成后最后调用.
// app.Use 不带 path 会匹配所有请求, 但已命中的业务路由如果直接返回且不调用 c.Next(),
// Fiber 会停止后续处理链, 因此这个最后兜底只会处理未命中的请求并统一返回 404.
// 如果提前调用本函数, 或业务路由的末尾 handler 主动调用 c.Next(), 该兜底会截断后续链并返回 404.
func SetupExceptionRouter(app *fiber.App) {
	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNotFound)
	})
}
