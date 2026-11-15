package middleware

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/response"
	"github.com/fufuok/pkg/web/fiber/tproxy"
)

type ForbiddenChecker = func(fiber.Ctx) bool

// 存放最近检查的白名单 IP 缓存快照
var whitelistCache atomic.Pointer[ipCache]

// UseWhitelistCache 重新设置白名单检查时缓存, 配置变化时可选再次调用, 由应用端 Start() Runtime() 调用
func UseWhitelistCache(capacity, lifetime uint32) error {
	return swapIPCache(&whitelistCache, capacity, lifetime)
}

// PurgeWhitelistCache 清空白名单缓存. 名单热更新后旧结果已按规则快照自动失效, 不再必须调用;
// 可在 Runtime() 中调用以尽早释放旧快照的缓存条目.
func PurgeWhitelistCache() {
	if c := whitelistCache.Load(); c != nil {
		c.lru.Purge()
	}
}

// CheckWhitelist 接口白名单检查
func CheckWhitelist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法来访(%s): ", config.AppName)
	return func(c fiber.Ctx) error {
		if !WhitelistChecker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// CheckWhitelistOr 校验接口白名单或自定义检查器
func CheckWhitelistOr(checker ForbiddenChecker, asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 禁止来访(%s): ", config.AppName)
	return func(c fiber.Ctx) error {
		if !WhitelistChecker(c) && !checker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// CheckWhitelistAnd 同时校验接口白名单和自定义检查器
func CheckWhitelistAnd(checker ForbiddenChecker, asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 禁止访问(%s): ", config.AppName)
	return func(c fiber.Ctx) error {
		if !WhitelistChecker(c) || !checker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// WhitelistChecker 是否通过了白名单检查, true 是白名单 (白名单为空时直接放行, 避免无谓解析客户端 IP).
func WhitelistChecker(c fiber.Ctx) bool {
	wl := config.WhitelistSnapshot()
	if len(wl) == 0 {
		return true
	}
	clientIP := tproxy.GetClientIP(c)
	// 缓存结果绑定名单快照, 热更新后旧结果 (含在途请求迟到的回填) 不会再放行或漏拦.
	return lookupIPList(whitelistCache.Load(), wl, clientIP)
}

func responseForbidden(c fiber.Ctx, msg string, asAPI bool) error {
	clientIP := tproxy.GetClientIP(c)
	msg += clientIP
	sampler.Info().
		Str("cip", c.IP()).Str("x_forwarded_for", c.Get(fiber.HeaderXForwardedFor)).
		Str(tproxy.HeaderXProxyClientIP, c.Get(tproxy.HeaderXProxyClientIP)).
		Str("method", c.Method()).Str("uri", c.OriginalURL()).Str("client_ip", clientIP).
		Msg(msg)

	if asAPI {
		return response.APIException(c, http.StatusForbidden, msg, nil)
	}
	return response.TxtException(c, http.StatusForbidden, msg)
}
