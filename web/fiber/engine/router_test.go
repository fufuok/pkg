package engine

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/utils/assert"
)

// TestSetupExceptionRouterKeepsRegisteredRoutes 验证异常路由按约定最后注册时,
// 不会覆盖已经注册并正常终止处理链的业务路由和系统路由.
func TestSetupExceptionRouterKeepsRegisteredRoutes(t *testing.T) {
	app := fiber.New()
	SetupSYSRouter(app)
	app.Get("/ok", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})
	app.Get("/api/users/:id", func(c fiber.Ctx) error {
		return c.SendString(c.Params("id"))
	})
	SetupExceptionRouter(app)

	tests := []struct {
		name   string
		method string
		path   string
		status int
		body   string
	}{
		{
			name:   "system ping",
			method: http.MethodGet,
			path:   "/ping",
			status: http.StatusOK,
			body:   "PONG",
		},
		{
			name:   "business route",
			method: http.MethodGet,
			path:   "/ok",
			status: http.StatusOK,
			body:   "OK",
		},
		{
			name:   "route params",
			method: http.MethodGet,
			path:   "/api/users/42",
			status: http.StatusOK,
			body:   "42",
		},
		{
			name:   "unmatched get",
			method: http.MethodGet,
			path:   "/missing",
			status: http.StatusNotFound,
		},
		{
			name:   "method mismatch falls through to fallback",
			method: http.MethodPost,
			path:   "/ok",
			status: http.StatusNotFound,
		},
		{
			name:   "unmatched post",
			method: http.MethodPost,
			path:   "/missing",
			status: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFiberResponse(t, app, tt.method, tt.path, tt.status, tt.body)
		})
	}
}

// TestSetupExceptionRouterMustBeRegisteredLast 验证异常路由的调用顺序是硬边界.
// 该兜底中间件无 path 且不会调用 c.Next(), 若先于业务路由注册, 后续业务路由会被截断为 404.
func TestSetupExceptionRouterMustBeRegisteredLast(t *testing.T) {
	app := fiber.New()
	SetupExceptionRouter(app)
	app.Get("/late", func(c fiber.Ctx) error {
		return c.SendString("LATE")
	})

	assertFiberResponse(t, app, http.MethodGet, "/late", http.StatusNotFound, "")
}

// assertFiberResponse 统一执行 Fiber app.Test 并校验状态码和可选响应体.
// body 为空字符串时只校验状态码, 便于覆盖 404 等不关心具体正文的分支.
func assertFiberResponse(t *testing.T, app *fiber.App, method, path string, status int, body string) {
	t.Helper()

	resp, err := app.Test(httptest.NewRequest(method, path, nil))
	assert.Nil(t, err)
	defer resp.Body.Close()
	assert.Equal(t, status, resp.StatusCode)

	if body == "" {
		return
	}
	actual, err := io.ReadAll(resp.Body)
	assert.Nil(t, err)
	assert.Equal(t, body, string(actual))
}
