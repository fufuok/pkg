package middleware

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/web/fiber/tproxy"
)

// TestWebLoggerTimeoutReturnsErrorWithoutLogging 验证真实截止只返回原错误, 不主动记录采样日志.
// 外层中间件可以接收错误, 但 Fiber 已捕获响应后不会再调用应用 ErrorHandler 或日志条件.
func TestWebLoggerTimeoutReturnsErrorWithoutLogging(t *testing.T) {
	logPointer := common.LogSampled()
	previous := *logPointer
	var output bytes.Buffer
	*logPointer = zerolog.New(&output)
	t.Cleanup(func() { *logPointer = previous })

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
	observed := make(chan error, 1)
	app := fiber.New(fiber.Config{ErrorHandler: func(fiber.Ctx, error) error {
		t.Error("captured timeout must not invoke the application error handler")
		return nil
	}})
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		observed <- err
		return err
	})
	app.Use(WebLogger(func(fiber.Ctx, time.Duration) bool {
		t.Error("captured timeout must not evaluate the request log condition")
		return false
	}, true), WithTimeout(10*time.Millisecond))
	app.Post("/slow", func(c fiber.Ctx) error {
		defer close(done)
		ctx := c.Context()
		<-ctx.Done()
		<-release
		return ctx.Err()
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/slow", strings.NewReader("payload")),
		fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	select {
	case got := <-observed:
		if got != fiber.ErrServiceUnavailable {
			t.Fatalf("outer middleware error = %v, want the original timeout error", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("outer middleware did not receive the timeout error")
	}
	if output.Len() != 0 {
		t.Fatalf("unexpected sampled timeout log: %s", output.Bytes())
	}
}

// TestWebLoggerTimeoutLeavesLocalsUntouched 验证真实截止后日志不再访问活动请求变量.
// 业务等待响应发送后才读取 Locals, 使旧日志器写入 IP 缓存的行为可确定性复现.
func TestWebLoggerTimeoutLeavesLocalsUntouched(t *testing.T) {
	for _, withBody := range []bool{false, true} {
		name := "without_body"
		if withBody {
			name = "with_body"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			observed := make(chan any, 1)
			app := fiber.New()
			app.Use(WebLogger(func(fiber.Ctx, time.Duration) bool {
				t.Error("timeout must not evaluate the request log condition")
				return true
			}, withBody))
			app.Use(WithTimeout(10 * time.Millisecond))
			app.Post("/slow", func(c fiber.Ctx) error {
				ctx := c.Context()
				<-ctx.Done()
				<-release
				observed <- c.Locals(tproxy.HeaderXProxyClientIP)
				return ctx.Err()
			})

			resp, err := app.Test(httptest.NewRequest(fiber.MethodPost, "/slow", strings.NewReader("payload")),
				fiber.TestConfig{Timeout: 5 * time.Second})
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", resp.StatusCode)
			}
			select {
			case value := <-observed:
				if value != nil {
					t.Fatalf("timeout logger modified worker Locals: %v", value)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed-out handler did not exit")
			}
		})
	}
}

// TestWebLoggerTimeoutConcurrentLocals 验证截止后的日志与仍运行的业务不存在 Locals 竞争.
// 业务只修改私有请求变量, 不写响应或请求头; race 检测应覆盖默认日志条件的错误分支.
func TestWebLoggerTimeoutConcurrentLocals(t *testing.T) {
	for range 5 {
		release := make(chan struct{})
		done := make(chan struct{})
		app := fiber.New()
		app.Use(WebLogger(nil), WithTimeout(10*time.Millisecond))
		app.Get("/slow", func(c fiber.Ctx) error {
			defer close(done)
			ctx := c.Context()
			for i := 0; ; i++ {
				select {
				case <-release:
					return ctx.Err()
				default:
					c.SetLocal(i%1024, i)
				}
			}
		})

		resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/slow", nil),
			fiber.TestConfig{Timeout: 5 * time.Second})
		close(release)
		if err != nil {
			t.Fatal(err)
		}
		if err = resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed-out handler did not exit")
		}
	}
}
