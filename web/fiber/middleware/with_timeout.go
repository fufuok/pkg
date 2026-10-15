package middleware

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	"github.com/fufuok/pkg/config"
)

// WithTimeoutDefault 使用 config.WebTimeout 构建中间件, 其他行为与 WithTimeout 一致.
func WithTimeoutDefault(timeoutErrors ...error) fiber.Handler {
	return WithTimeout(config.WebTimeout, timeoutErrors...)
}

// WithTimeout 为后续 Handler 设置处理期限, 默认沿用 Fiber 的 503 超时响应.
// Handler 应监听 c.Context().Done() 并及时结束; 超时回包不会强制终止业务逻辑.
// timeoutErrors 按错误链匹配自定义超时错误, 普通错误继续交由应用 ErrorHandler 处理.
func WithTimeout(dur time.Duration, timeoutErrors ...error) fiber.Handler {
	h := func(c fiber.Ctx) error {
		return c.Next()
	}
	return timeout.New(h, timeout.Config{
		Timeout: dur,
		Errors:  timeoutErrors,
	})
}
