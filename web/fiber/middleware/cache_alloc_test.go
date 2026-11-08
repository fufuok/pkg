//go:build !race

package middleware

import (
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// TestFiberCacheBypassZeroAlloc 验证携带 Cookie 的旁路请求, 无 Vary 头 no-store 直通请求与热命中均不产生堆分配.
// race 检测器会对 fasthttp Header.All 的迭代器闭包注入插桩分配, 因此仅在常规编译下锁定 0 分配契约.
func TestFiberCacheBypassZeroAlloc(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	app.Get("/cookie", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/nostore", func(c fiber.Ctx) error {
		c.Set("Cache-Control", "no-store")
		return c.SendString("dynamic")
	})
	app.Get("/hit", func(c fiber.Ctx) error {
		c.Set("Cache-Control", "public, max-age=60")
		return c.JSON(fiber.Map{"ok": true})
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

	// 热命中: 回放清除默认 Content-Type 等现有表示头时, 待删名称经栈缓冲按原始字节删除, 不分配字符串.
	var hitCtx fasthttp.RequestCtx
	hitCtx.Request.Header.SetMethod(fiber.MethodGet)
	hitCtx.Request.SetRequestURI("/hit")
	hitCtx.Request.Header.Set("Host", "example.com")
	app.Handler()(&hitCtx)
	hitAllocs := testing.AllocsPerRun(200, func() {
		hitCtx.Response.Reset()
		app.Handler()(&hitCtx)
	})
	if hitAllocs != 0 || hitCtx.Response.Header.Peek("Age") == nil {
		t.Fatalf("cache hit allocs = %v (Age = %q), want 0 allocs from cache", hitAllocs, hitCtx.Response.Header.Peek("Age"))
	}
}
