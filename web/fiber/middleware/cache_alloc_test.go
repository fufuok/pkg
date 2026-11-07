//go:build !race

package middleware

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// TestFiberCacheBypassZeroAlloc 验证携带 Cookie 的旁路请求与无 Vary 头 no-store 直通请求不产生堆分配.
// race 检测器会对 fasthttp Header.All 的迭代器闭包注入插桩分配, 因此仅在常规编译下锁定 0 分配契约.
func TestFiberCacheBypassZeroAlloc(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	app.Get("/cookie", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/nostore", func(c fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")
		return c.SendString("dynamic")
	})

	var cookieCtx fasthttp.RequestCtx
	cookieCtx.Request.Header.SetMethod(fiber.MethodGet)
	cookieCtx.Request.SetRequestURI("/cookie")
	cookieCtx.Request.Header.Set("Host", "example.com")
	cookieCtx.Request.Header.Set("Accept", "application/json")
	cookieCtx.Request.Header.Set("Cookie", "sid=123456")
	app.Handler()(&cookieCtx)

	cookieAllocs := testing.AllocsPerRun(200, func() {
		cookieCtx.Response.Reset()
		app.Handler()(&cookieCtx)
	})
	if cookieAllocs != 0 {
		t.Fatalf("cookie bypass allocs = %v, want 0", cookieAllocs)
	}

	var noStoreCtx fasthttp.RequestCtx
	noStoreCtx.Request.Header.SetMethod(fiber.MethodGet)
	noStoreCtx.Request.SetRequestURI("/nostore")
	noStoreCtx.Request.Header.Set("Host", "example.com")
	app.Handler()(&noStoreCtx)

	noStoreAllocs := testing.AllocsPerRun(200, func() {
		noStoreCtx.Response.Reset()
		app.Handler()(&noStoreCtx)
	})
	if noStoreAllocs != 0 {
		t.Fatalf("no-store bypass allocs = %v, want 0", noStoreAllocs)
	}
}
