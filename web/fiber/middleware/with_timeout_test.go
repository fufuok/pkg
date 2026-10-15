package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/config"
)

// Ref: https://github.com/gofiber/fiber/tree/master/middleware/timeout
var ErrFooTimeOut = errors.New("foo context canceled")

// TestWithTimeoutResult 验证两个入口都沿错误链识别超时, 不误映射未注册的普通错误.
// Handler 立即返回, 避免实际截止与完成事件竞争掩盖自定义错误匹配分支.
func TestWithTimeoutResult(t *testing.T) {
	oldTimeout := config.WebTimeout
	config.WebTimeout = time.Minute
	t.Cleanup(func() { config.WebTimeout = oldTimeout })
	customErr := fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrFooTimeOut))
	for _, entry := range []struct {
		name string
		new  func(...error) fiber.Handler
	}{
		{name: "explicit", new: func(errs ...error) fiber.Handler { return WithTimeout(time.Minute, errs...) }},
		{name: "default", new: WithTimeoutDefault},
	} {
		t.Run(entry.name, func(t *testing.T) {
			for _, tt := range []struct {
				name          string
				result        error
				timeoutErrors []error
				status        int
				body          string
			}{
				{name: "success", status: fiber.StatusOK},
				{name: "deadline", result: fmt.Errorf("wrapped: %w", context.DeadlineExceeded), status: fiber.StatusServiceUnavailable, body: fiber.ErrServiceUnavailable.Message},
				{name: "custom", result: customErr, timeoutErrors: []error{ErrFooTimeOut}, status: fiber.StatusServiceUnavailable, body: fiber.ErrServiceUnavailable.Message},
				{name: "unregistered", result: customErr, status: fiber.StatusInternalServerError, body: customErr.Error()},
			} {
				t.Run(tt.name, func(t *testing.T) {
					app := fiber.New()
					app.Use(entry.new(tt.timeoutErrors...))
					app.Get("/", func(fiber.Ctx) error { return tt.result })
					resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
					if err != nil {
						t.Fatalf("test timeout handler: %v", err)
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatalf("read timeout response: %v", err)
					}
					if resp.StatusCode != tt.status || string(body) != tt.body {
						t.Fatalf("response = %d %q, want %d %q", resp.StatusCode, body, tt.status, tt.body)
					}
				})
			}
		})
	}
}

// TestWithTimeoutDeadlineStatus 固定命中真正截止分支, 同时覆盖业务吞掉取消错误的情况.
// 响应校验完成后才允许 Handler 退出, 排除返回结果与截止信号同时就绪的偶然时序.
func TestWithTimeoutDeadlineStatus(t *testing.T) {
	for _, result := range []error{context.DeadlineExceeded, nil} {
		t.Run(fmt.Sprint(result), func(t *testing.T) {
			release := make(chan struct{})
			done := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(func() {
				unblock()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("timed-out handler did not exit")
				}
			})
			app := fiber.New()
			app.Use(WithTimeout(10 * time.Millisecond))
			app.Get("/", func(c fiber.Ctx) error {
				defer close(done)
				ctx := c.Context()
				<-ctx.Done()
				<-release
				return result
			})
			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
			if err != nil {
				t.Fatalf("test expired handler: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read expired response: %v", err)
			}
			if resp.StatusCode != fiber.StatusServiceUnavailable || string(body) != fiber.ErrServiceUnavailable.Message {
				t.Fatalf("timeout response = %d %q, want 503 %q", resp.StatusCode, body, fiber.ErrServiceUnavailable.Message)
			}
			unblock()
		})
	}
}
