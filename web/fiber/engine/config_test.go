package engine

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/fiber/middleware"
	"github.com/fufuok/pkg/web/fiber/tproxy"
)

// TestNewAppBodyLimit 验证引擎使用配置层归一化后的上限, 并保持正数上限.
// 5MiB 上传可区分 pkg 的 8MiB 与 Fiber 的 4MiB 回退; 下载响应不受请求体上限限制.
func TestNewAppBodyLimit(t *testing.T) {
	for _, limit := range []int{-2, -1, 0, 1024} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			cfg := config.NormalizeWebConf(config.WebConf{}, config.WebConf{BodyLimit: limit})
			app := newApp(func(app *fiber.App) *fiber.App {
				app.Post("/upload", func(c fiber.Ctx) error { return c.SendString("ok") })
				app.Get("/download", func(c fiber.Ctx) error { return c.SendString(strings.Repeat("x", 5<<20)) })
				return app
			}, cfg)
			want := limit
			if want <= 0 {
				want = config.WebBodyLimit
			}
			if got := app.Config().BodyLimit; got != want {
				t.Fatalf("BodyLimit = %d, want %d", got, want)
			}
			if limit <= 0 {
				resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(strings.Repeat("x", 5<<20))))
				if err != nil {
					t.Fatal(err)
				}
				if err = resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("upload status = %d, want 200", resp.StatusCode)
				}
				return
			}

			// 小请求体上限不应截断较大的下载正文.
			if resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(strings.Repeat("x", 2048)))); !errors.Is(err, fasthttp.ErrBodyTooLarge) {
				if resp != nil {
					_ = resp.Body.Close()
				}
				t.Fatalf("oversized upload error = %v, want body too large", err)
			}
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/download", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			n, err := io.Copy(io.Discard, resp.Body)
			if err != nil || n != 5<<20 || resp.StatusCode != http.StatusOK {
				t.Fatalf("download status/bytes/error = %d/%d/%v", resp.StatusCode, n, err)
			}
		})
	}
}

// TestNewAppIPValidation 用固定 TCP 来源验证配置映射、可信链选择及真实名单行为.
// 左侧伪造地址不能覆盖右侧首个不可信地址, 不可信来源的代理头必须被忽略.
func TestNewAppIPValidation(t *testing.T) {
	const chain = "203.0.113.7, 198.51.100.10, 10.0.0.1"
	_, blocked, err := net.ParseCIDR("198.51.100.10/32")
	if err != nil {
		t.Fatal(err)
	}
	previous := config.Blacklist
	config.Blacklist = map[*net.IPNet]int64{blocked: 0}
	t.Cleanup(func() { config.Blacklist = previous })

	for _, tt := range []struct {
		name       string
		peer       string
		header     string
		validate   bool
		trust      bool
		wantIP     string
		wantStatus int
	}{
		{"trusted_chain", "10.0.0.2", chain, true, true, "198.51.100.10", fiber.StatusForbidden},
		{"default_validation", "10.0.0.2", chain, false, true, chain, fiber.StatusOK},
		{"untrusted_peer", "198.51.100.20", chain, true, true, "198.51.100.20", fiber.StatusOK},
		{"trust_disabled", "10.0.0.2", chain, true, false, "10.0.0.2", fiber.StatusOK},
		{"invalid_chain", "10.0.0.2", "invalid, bogus", true, true, "10.0.0.2", fiber.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var gotIP, gotClientIP string
			app := newApp(func(app *fiber.App) *fiber.App {
				app.Use(func(c fiber.Ctx) error {
					gotIP, gotClientIP = c.IP(), tproxy.GetClientIP(c)
					return c.Next()
				})
				app.Use(middleware.CheckBlacklist(false))
				app.Get("/protected", func(c fiber.Ctx) error { return c.SendString("ok") })
				return app
			}, config.WebConf{
				BodyLimit:               config.WebBodyLimit,
				ProxyHeader:             fiber.HeaderXForwardedFor,
				EnableTrustedProxyCheck: tt.trust,
				EnableIPValidation:      tt.validate,
				TrustedProxies:          []string{"10.0.0.0/24"},
			})
			var req fasthttp.Request
			req.SetRequestURI("http://example.test/protected")
			req.Header.Set(fiber.HeaderXForwardedFor, tt.header)
			var ctx fasthttp.RequestCtx
			ctx.Init(&req, &net.TCPAddr{IP: net.ParseIP(tt.peer), Port: 12345}, nil)
			app.Handler()(&ctx)
			if gotIP != tt.wantIP || gotClientIP != tt.wantIP || ctx.Response.StatusCode() != tt.wantStatus {
				t.Fatalf("IP/clientIP/status = %q/%q/%d, want %q/%q/%d", gotIP, gotClientIP,
					ctx.Response.StatusCode(), tt.wantIP, tt.wantIP, tt.wantStatus)
			}
		})
	}
}
