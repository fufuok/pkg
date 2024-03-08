package middleware

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/fiber/proxy"
)

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if BlacklistChecker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// BlacklistChecker 是否存在于黑名单, true 是黑名单 (黑名单为空时: 放过, false)
func BlacklistChecker(c *fiber.Ctx) bool {
	clientIP := proxy.GetClientIP(c)
	if len(config.Blacklist) > 0 {
		_, ok := common.LookupIPNetsString(clientIP, config.Blacklist)
		return ok
	}
	return false
}
