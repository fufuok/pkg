//go:build !race

package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/gofiber/fiber/v3"
)

// Test_TimeoutUseWithCustomError 在 -race 模式下跳过。
// 该测试在 fiber v3 timeout 中间件 + WithTimeout 包装下存在已知 race，
// 仅在非 race 模式下执行，避免 CI/Windows/WSL2 环境下误报。
func Test_TimeoutUseWithCustomError(t *testing.T) {
	app := fiber.New()
	app.Use(WithTimeout(200*time.Millisecond, ErrFooTimeOut))
	h := func(c fiber.Ctx) error {
		sleepTime, _ := time.ParseDuration(c.Params("sleepTime") + "ms")
		if err := sleepWithContext(c.Context(), sleepTime, context.DeadlineExceeded); err != nil {
			return fmt.Errorf("%w: l2 wrap", fmt.Errorf("%w: l1 wrap ", err))
		}
		return nil
	}
	group := app.Group("/group", WithTimeout(100*time.Millisecond, ErrFooTimeOut))
	{
		group.Get("/:sleepTime", h)
	}
	app.Get("/test/:sleepTime", WithTimeout(100*time.Millisecond, ErrFooTimeOut), h)
	app.Get("/:sleepTime", h)
	testTimeout := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusRequestTimeout, resp.StatusCode, "Status code")
		_ = resp.Body.Close()
	}
	testSucces := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Status code")
		_ = resp.Body.Close()
	}
	testTimeout("/300")
	testTimeout("/group/150")
	testTimeout("/test/150")
	testSucces("/150")
	testSucces("/group/30")
	testSucces("/test/30")
}

// Test_WithTimeoutSkipTimeoutStatus 在 -race 模式下跳过。
// 该测试在 fiber v3 timeout 中间件 + WithTimeout 包装下存在已知 race，
// 仅在非 race 模式下执行，避免 CI/Windows/WSL2 环境下误报。
func Test_WithTimeoutSkipTimeoutStatus(t *testing.T) {
	app := fiber.New()
	app.Use(WithTimeout(200*time.Millisecond, ErrFooTimeOut))
	h := func(c fiber.Ctx) error {
		sleepTime, _ := time.ParseDuration(c.Params("sleepTime") + "ms")
		if err := sleepWithContext(c.Context(), sleepTime, context.DeadlineExceeded); err != nil {
			return nil
		}
		return c.SendString("OK")
	}
	group := app.Group("/group", WithTimeout(100*time.Millisecond, ErrFooTimeOut))
	{
		group.Get("/:sleepTime", h)
	}
	app.Get("/test/:sleepTime", WithTimeout(100*time.Millisecond, ErrFooTimeOut), h)
	app.Get("/:sleepTime", h)
	testTimeout := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusRequestTimeout, resp.StatusCode, "Status code")
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, nil, err)
		assert.Equal(t, fiber.ErrRequestTimeout.Message, string(body))
		_ = resp.Body.Close()
	}
	testSucces := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Status code")
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, nil, err)
		assert.Equal(t, "OK", string(body))
		_ = resp.Body.Close()
	}
	testTimeout("/300")
	testTimeout("/group/150")
	testTimeout("/test/150")
	testSucces("/150")
	testSucces("/group/30")
	testSucces("/test/30")
}

func sleepWithContext(ctx context.Context, d time.Duration, te error) error {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		return te
	case <-timer.C:
	}
	return nil
}
