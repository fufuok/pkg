package middleware

import (
	"net"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/fufuok/pkg/config"
)

func resetFiberIPTestState(t *testing.T) {
	t.Helper()
	config.InitTester()
	t.Cleanup(func() {
		_ = UseWhitelistCache(0, 0)
		_ = UseBlacklistCache(0, 0)
		config.StopTester()
	})
	_ = UseWhitelistCache(0, 0)
	_ = UseBlacklistCache(0, 0)
}

// TestIPCacheReuseAndConcurrentReload 验证同参数热重载复用分片缓存但清空旧项, 且并发读写零竞争.
func TestIPCacheReuseAndConcurrentReload(t *testing.T) {
	resetFiberIPTestState(t)
	_, allowNet, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	config.Whitelist = map[*net.IPNet]int64{allowNet: 1}

	if err := UseWhitelistCache(128, 60); err != nil {
		t.Fatal(err)
	}
	first := whitelistCache.Load()
	if first == nil {
		t.Fatal("expected initialized whitelist cache")
	}

	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)
	ctx.Request().Header.Set(HeaderXProxyClientIPValue(), "invalid")
	ctx.RequestCtx().RemoteAddr()

	first.lru.Add("10.1.2.3", true)
	if _, ok := first.lru.Get("10.1.2.3"); !ok {
		t.Fatal("expected cached entry before reload")
	}
	if err := UseWhitelistCache(128, 60); err != nil {
		t.Fatal(err)
	}
	second := whitelistCache.Load()
	if second != first {
		t.Fatal("unchanged capacity/lifetime should reuse ipCache instance and purge entries")
	}
	if _, ok := second.lru.Get("10.1.2.3"); ok {
		t.Fatal("reused cache should have purged stale entries on reload")
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		reqApp := fiber.New()
		reqCtx := reqApp.AcquireCtx(&fasthttp.RequestCtx{})
		defer reqApp.ReleaseCtx(reqCtx)
		for range 500 {
			_ = WhitelistChecker(reqCtx)
			_ = CacheStats()
		}
	})
	for i := range 100 {
		if i%3 == 0 {
			_ = UseWhitelistCache(0, 0)
		} else {
			_ = UseWhitelistCache(uint32(64+(i%2)*64), 60)
		}
	}
	wg.Wait()
}

func HeaderXProxyClientIPValue() string {
	return "X-Proxy-Client-IP"
}

// TestEmptyBlacklistZeroAllocFastPath 验证生产常态空黑名单跳过客户端 IP 解析且零分配.
func TestEmptyBlacklistZeroAllocFastPath(t *testing.T) {
	resetFiberIPTestState(t)
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)
	ctx.Request().Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")

	allocs := testing.AllocsPerRun(100, func() {
		if BlacklistChecker(ctx) {
			t.Fatal("empty blacklist must return false")
		}
		if !WhitelistChecker(ctx) {
			t.Fatal("empty whitelist must return true")
		}
	})
	if allocs != 0 {
		t.Fatalf("empty IP list check allocs = %v, want 0", allocs)
	}
}

func BenchmarkBlacklistCheckerEmpty(b *testing.B) {
	oldBL := config.Blacklist
	config.Blacklist = nil
	b.Cleanup(func() { config.Blacklist = oldBL })

	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = BlacklistChecker(ctx)
	}
}
