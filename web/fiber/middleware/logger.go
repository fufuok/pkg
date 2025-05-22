package middleware

import (
	"runtime/debug"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/tproxy"
)

// LogCondition 日志记录条件计算器
type LogCondition func(c *fiber.Ctx, elapsed time.Duration) bool

func DefaultLogCondition(c *fiber.Ctx, elapsed time.Duration) bool {
	return elapsed > config.WebLogSlowResponse || c.Response().StatusCode() >= config.WebLogMinStatusCode
}

// WebLogger Web 日志
func WebLogger(cond LogCondition, withBody ...bool) fiber.Handler {
	if cond == nil {
		cond = DefaultLogCondition
	}
	withbody := len(withBody) > 0 && withBody[0]
	return func(c *fiber.Ctx) error {
		start := time.Now()

		// Handle request, store err for logging
		chainErr := c.Next()
		elapsed := time.Since(start)

		// Manually call error handler
		if chainErr != nil {
			log := sampler.Error().Err(chainErr).
				Str("elapsed", elapsed.String()).
				Str("client_ip", tproxy.GetClientIP(c)).Str("method", c.Method())
			if withbody {
				log.Bytes("body", c.Body())
			}
			log.Msg(c.OriginalURL())
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		if cond(c, elapsed) {
			log := sampler.Warn().
				Str("client_ip", tproxy.GetClientIP(c)).Str("elapsed", elapsed.String()).
				Str("method", c.Method()).Int("http_code", c.Response().StatusCode())
			if withbody {
				log.Bytes("body", c.Body())
			}
			log.Msg(c.OriginalURL())
		}
		return nil
	}
}

// RecoverLogger Recover 并记录日志
func RecoverLogger() fiber.Handler {
	return func(c *fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				var ok bool
				if err, ok = r.(*fiber.Error); !ok {
					// 屏蔽错误细节, 让全局错误处理响应 500
					err = &fiber.Error{
						Code:    500,
						Message: "Internal Server Error",
					}
				}
				sampler.Error().
					Bytes("stack", debug.Stack()).
					Bytes("body", c.Body()).
					Str("client_ip", c.IP()).
					Str("method", c.Method()).Str("uri", c.OriginalURL()).
					Msgf("Recovered from panic: %s", r)
			}
		}()
		return c.Next()
	}
}
