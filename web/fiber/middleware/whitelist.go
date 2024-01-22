package middleware

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/proxy"
	"github.com/fufuok/pkg/web/fiber/response"
)

// CheckWhitelist 接口白名单检查
func CheckWhitelist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法来访(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if len(config.Whitelist) > 0 {
			clientIP := proxy.GetClientIP(c)
			if _, ok := common.LookupIPNetsString(clientIP, config.Whitelist); !ok {
				msg := errMsg + clientIP
				sampler.Info().
					Str("cip", c.IP()).Str("x_forwarded_for", c.Get(fiber.HeaderXForwardedFor)).
					Str(proxy.HeaderXProxyClientIP, c.Get(proxy.HeaderXProxyClientIP)).
					Str("method", c.Method()).Str("uri", c.OriginalURL()).Str("client_ip", clientIP).
					Msg(msg)
				if asAPI {
					return response.APIException(c, http.StatusForbidden, msg, nil)
				} else {
					return response.TxtException(c, http.StatusForbidden, msg)
				}
			}
		}
		return c.Next()
	}
}
