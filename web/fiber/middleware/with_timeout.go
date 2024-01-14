package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/timeout"

	"github.com/fufuok/pkg/config"
)

func WithTimeoutDefault(timeoutErrors ...error) fiber.Handler {
	return WithTimeout(config.WebTimeout, timeoutErrors...)
}

// WithTimeout 附加超时上下文, Handler 需要使用 <-ctx.UserContext().Done
func WithTimeout(dur time.Duration, timeoutErrors ...error) fiber.Handler {
	h := func(c *fiber.Ctx) error {
		return c.Next()
	}
	return timeout.NewWithContext(h, dur, timeoutErrors...)
}
