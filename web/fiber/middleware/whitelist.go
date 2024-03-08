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

type ForbiddenChecker = func(*fiber.Ctx) bool

// CheckWhitelist 接口白名单检查
func CheckWhitelist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法来访(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if !WhitelistChecker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// CheckWhitelistOr 校验接口白名单或自定义检查器
func CheckWhitelistOr(checker ForbiddenChecker, asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 禁止来访(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if !WhitelistChecker(c) && !checker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// CheckWhitelistAnd 同时校验接口白名单和自定义检查器
func CheckWhitelistAnd(checker ForbiddenChecker, asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 禁止访问(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if !WhitelistChecker(c) || !checker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// WhitelistChecker 是否通过了白名单检查, true 是白名单 (白名单为空时: 通过, true)
func WhitelistChecker(c *fiber.Ctx) bool {
	clientIP := proxy.GetClientIP(c)
	if len(config.Whitelist) > 0 {
		_, ok := common.LookupIPNetsString(clientIP, config.Whitelist)
		return ok
	}
	return true
}

func responseForbidden(c *fiber.Ctx, msg string, asAPI bool) error {
	clientIP := proxy.GetClientIP(c)
	msg += clientIP
	sampler.Info().
		Str("cip", c.IP()).Str("x_forwarded_for", c.Get(fiber.HeaderXForwardedFor)).
		Str(proxy.HeaderXProxyClientIP, c.Get(proxy.HeaderXProxyClientIP)).
		Str("method", c.Method()).Str("uri", c.OriginalURL()).Str("client_ip", clientIP).
		Msg(msg)

	if asAPI {
		return response.APIException(c, http.StatusForbidden, msg, nil)
	}
	return response.TxtException(c, http.StatusForbidden, msg)
}
