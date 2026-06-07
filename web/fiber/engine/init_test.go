package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/utils/assert"

	"github.com/fufuok/pkg/config"
)

// TestNewAppRecoversPanic 回归测试 (#1, fiber): 恢复中间件必须先于业务路由注册,
// 才能捕获路由 panic, 经全局 ErrorHandler 响应 500.
//
// 守护点: 若恢复中间件被误移回 setup 之后注册, 业务路由 panic 不会被恢复, /panic 将无法得到 500.
func TestNewAppRecoversPanic(t *testing.T) {
	setup := func(app *fiber.App) *fiber.App {
		app.Get("/panic", func(fiber.Ctx) error {
			panic("boom: simulated handler panic")
		})
		app.Get("/ok", func(c fiber.Ctx) error {
			return c.SendString("OK")
		})
		return app
	}

	app := newApp(setup, config.WebConf{})

	// panic 路由应被恢复中间件捕获, 经 ErrorHandler 响应 500
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/panic", nil))
	assert.Nil(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// 正常路由不受影响, 仍返回 200, 说明恢复后进程存活可继续服务
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
