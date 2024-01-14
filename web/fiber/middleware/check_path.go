package middleware

import (
	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/config"
)

// CheckPathOr404 检查 URL 是否为配置中的路由
func CheckPathOr404() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 注: StatsPath 设定值必须以 / 开头
		v := c.Params("*")
		if v == "" {
			return c.SendStatus(fiber.StatusNotFound)
		}
		u := "/" + v
		p := config.Config().WebConf.StatsPath
		if u != p {
			return c.SendStatus(fiber.StatusNotFound)
		}

		return c.Next()
	}
}
