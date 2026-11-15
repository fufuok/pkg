package middleware

import (
	"net"
	"sync"
	"testing"
	"unsafe"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/fiber/tproxy"
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

	first.lru.Add("10.1.2.3", ipVerdict{ok: true})
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

// TestIPCacheBoundToRuleSnapshot 验证名单缓存绑定规则快照: 热更新不调用 Purge 也立即生效,
// 更新前读旧快照的在途请求在 Purge 之后迟到回填, 也不会让已撤销的白名单继续放行或新增黑名单漏拦.
func TestIPCacheBoundToRuleSnapshot(t *testing.T) {
	resetFiberIPTestState(t)
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(ctx)
	ip := tproxy.GetClientIP(ctx)
	host := net.ParseIP(ip)
	if host == nil {
		t.Fatalf("unexpected client ip %q", ip)
	}
	mask := net.CIDRMask(8*len(host), 8*len(host))
	ipRules := map[*net.IPNet]int64{{IP: host, Mask: mask}: 1}
	other := func() map[*net.IPNet]int64 {
		_, n, err := net.ParseCIDR("192.0.2.0/24")
		if err != nil {
			t.Fatal(err)
		}
		return map[*net.IPNet]int64{n: 1}
	}
	if err := UseWhitelistCache(128, 0); err != nil {
		t.Fatal(err)
	}
	if err := UseBlacklistCache(128, 0); err != nil {
		t.Fatal(err)
	}

	// 白名单: 旧规则放行并写入缓存; 换成新规则后不 Purge 也必须拒绝.
	config.Whitelist = ipRules
	if !WhitelistChecker(ctx) {
		t.Fatalf("old whitelist should allow %s", ip)
	}
	newRules := other()
	config.Whitelist = newRules
	if WhitelistChecker(ctx) {
		t.Fatal("cached verdict of old whitelist must not survive rule reload")
	}
	// 在途请求按旧快照算完, 在发布新规则并 Purge 之后才回填.
	PurgeWhitelistCache()
	_ = lookupIPList(whitelistCache.Load(), ipRules, ip)
	if WhitelistChecker(ctx) {
		t.Fatal("late backfill from old whitelist snapshot revived revoked access")
	}

	// 黑名单: 旧规则不拦, 新规则拦截, 迟到回填的 "不在黑名单" 不得生效.
	oldBlack := other()
	config.Blacklist = oldBlack
	if BlacklistChecker(ctx) {
		t.Fatalf("old blacklist should not block %s", ip)
	}
	config.Blacklist = ipRules
	PurgeBlacklistCache()
	_ = lookupIPList(blacklistCache.Load(), oldBlack, ip)
	if !BlacklistChecker(ctx) {
		t.Fatal("late backfill from old blacklist snapshot let a newly blocked IP pass")
	}
}

// TestIPCacheKeyDetachedFromRequestBuffer 验证缓存键与请求缓冲解耦: fiber 代理头取得的 IP 是零拷贝字符串,
// 请求结束后底层字节被复用改写, 缓存中的键必须保持写入时的内容.
func TestIPCacheKeyDetachedFromRequestBuffer(t *testing.T) {
	resetFiberIPTestState(t)
	if err := UseWhitelistCache(128, 0); err != nil {
		t.Fatal(err)
	}
	_, n, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	rules := map[*net.IPNet]int64{n: 1}
	cache := whitelistCache.Load()
	buf := []byte("10.1.2.3")
	if !lookupIPList(cache, rules, unsafe.String(&buf[0], len(buf))) {
		t.Fatal("10.1.2.3 should be allowed")
	}
	copy(buf, "99.9.9.9") // 模拟请求缓冲被下一个请求复用
	if v, ok := cache.lru.Get("10.1.2.3"); !ok || !v.ok {
		t.Fatal("cached key was mutated together with the request buffer")
	}
}
