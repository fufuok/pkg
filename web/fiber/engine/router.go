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
