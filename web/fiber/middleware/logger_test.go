package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestWebLoggerPropagatesRouteNotFound 验证分组中间件命中后, 未匹配后续路由时仍返回 404.
// 该场景覆盖 Fiber 在 c.Next() 中返回 ErrNotFound 的边界, WebLogger 不应将其改写为 500.
func TestWebLoggerPropagatesRouteNotFound(t *testing.T) {
	app := fiber.New()
	api := app.Group("/api", WebLogger(nil))
	api.Get("/ok", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(fiber.MethodGet, "/api/missing", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})

	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, fiber.StatusNotFound)
	}
}

// TestWebLoggerPropagatesMethodNotAllowed 验证方法不匹配时保留 Fiber 的 405 语义.
// 该用例防止 WebLogger 将路由层的 ErrMethodNotAllowed 误转换成内部错误.
func TestWebLoggerPropagatesMethodNotAllowed(t *testing.T) {
	app := fiber.New()
	api := app.Group("/api", WebLogger(nil))
	api.Get("/ok", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest(fiber.MethodPost, "/api/ok", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})

	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, fiber.StatusMethodNotAllowed)
	}
}

// TestWebLoggerPropagatesHandlerError 验证业务 handler 返回错误时交由 Fiber ErrorHandler 统一处理.
// 该用例固定 WebLogger 的职责边界: 记录错误但不覆盖调用方自定义的错误响应策略.
func TestWebLoggerPropagatesHandlerError(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(fiber.StatusTeapot).SendString(err.Error())
		},
	})
	app.Use(WebLogger(nil))
	app.Get("/err", func(fiber.Ctx) error {
		return fiber.ErrBadRequest
	})

	req := httptest.NewRequest(fiber.MethodGet, "/err", nil)
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})

	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	if resp.StatusCode != fiber.StatusTeapot {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, fiber.StatusTeapot)
	}
}
