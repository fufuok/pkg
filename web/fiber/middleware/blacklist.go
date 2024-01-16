package middleware

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/proxy"
	"github.com/fufuok/pkg/web/fiber/response"
)

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if len(config.Blacklist) > 0 {
			clientIP := proxy.GetClientIP(c)
			if _, ok := common.LookupIPNets(clientIP, config.Blacklist); ok {
				msg := errMsg + clientIP
				sampler.Info().
					Str("cip", c.IP()).Str("x_forwarded_for", c.Get(fiber.HeaderXForwardedFor)).
					Str(proxy.HeaderXProxyClientIP, c.Get(proxy.HeaderXProxyClientIP)).
					Str("method", c.Method()).Str("uri", c.OriginalURL()).Str("client_ip", clientIP).
					Msg(msg)
				if asAPI {
					return response.APIFailure(c, msg, nil)
				} else {
					return response.TxtMsg(c, msg)
				}
			}
		}
		return c.Next()
	}
}
