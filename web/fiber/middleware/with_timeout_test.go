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

// TestWithTimeoutWorker 直接验证工作协程的结果规范化, 即完成分支实际执行的逻辑: 业务错误原样返回,
// 返回 nil 但入口 context 已截止或被上游取消时改为 fiber.ErrServiceUnavailable, 内层派生 context 收尾 cancel 不影响结果.
// 截止与返回同时就绪时 Fiber 选择哪个分支取决于调度, 端到端测试无法稳定固定完成分支.
func TestWithTimeoutWorker(t *testing.T) {
	businessErr := errors.New("business failure")
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		entry   context.Context
		result  error
		derived bool
		want    error
	}{
		{"live_nil", context.Background(), nil, false, nil},
		{"expired_nil", expired, nil, false, fiber.ErrServiceUnavailable},
		{"canceled_nil", canceled, nil, false, fiber.ErrServiceUnavailable},
		{"expired_error", expired, businessErr, false, businessErr},
		{"inner_derived_cancel", context.Background(), nil, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var got error
			app.Get("/", func(c fiber.Ctx) error {
				c.SetContext(tc.entry)
				got = timeoutWorker(c)
				return nil
			}, func(c fiber.Ctx) error {
				if tc.derived {
					ctx, cancel := context.WithCancel(c.Context())
					c.SetContext(ctx)
					cancel()
				}
				return tc.result
			})
			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if !errors.Is(got, tc.want) || (tc.want == nil) != (got == nil) {
				t.Fatalf("worker result = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWithTimeoutNilAfterDeadline 验证业务在工作协程 context 截止或被上游取消后返回 nil 时, 无论 Fiber
// 选择哪个分支都得到同一超时响应, 不会把取消后的残缺结果当作正常 200, 也不暴露内部取消文本.
func TestWithTimeoutNilAfterDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		cancel  bool
	}{{"deadline", time.Millisecond, false}, {"parent_canceled", time.Minute, true}} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				if tc.cancel {
					ctx, cancel := context.WithCancel(c.Context())
					cancel()
					c.SetContext(ctx)
				}
				return c.Next()
			}, WithTimeout(tc.timeout))
			app.Get("/", func(c fiber.Ctx) error {
				<-c.Context().Done()
				return nil
			})
			for range 50 {
				resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != fiber.StatusServiceUnavailable || string(body) != fiber.ErrServiceUnavailable.Message {
					t.Fatalf("response = %d %q, want 503 %q", resp.StatusCode, body, fiber.ErrServiceUnavailable.Message)
				}
			}
		})
	}
}

// TestWithTimeoutErrorsSnapshot 验证 timeoutErrors 在构造时保存快照: 调用方之后改写自己的切片不影响匹配.
func TestWithTimeoutErrorsSnapshot(t *testing.T) {
	errs := []error{ErrFooTimeOut}
	app := fiber.New()
	app.Use(WithTimeout(time.Minute, errs...))
	errs[0] = errors.New("replaced")
	app.Get("/", func(fiber.Ctx) error { return ErrFooTimeOut })
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// TestWithTimeoutNonPositive 验证非正时长完全禁用期限: 同步执行, 不设置缓存透传标记, 外层 context 已结束时
// 也不改写业务返回值.
func TestWithTimeoutNonPositive(t *testing.T) {
	app := fiber.New()
	var calls int
	app.Use(func(c fiber.Ctx) error {
		ctx, cancel := context.WithCancel(c.Context())
		cancel()
		c.SetContext(ctx)
		return c.Next()
	}, WithTimeout(0), func(c fiber.Ctx) error {
		if c.Locals(asyncTimeoutKey{}) != nil {
			t.Error("non-positive timeout set the async marker")
		}
		return c.Next()
	})
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		return c.SendString("ok")
	})
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK || calls != 1 {
		t.Fatalf("status = %d, handler calls = %d", resp.StatusCode, calls)
	}
}
