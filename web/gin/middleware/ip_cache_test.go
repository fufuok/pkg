package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
)

func resetGinIPTestState(t *testing.T) {
	t.Helper()
	oldWL, oldBL := config.Whitelist, config.Blacklist
	t.Cleanup(func() {
		config.Whitelist = oldWL
		config.Blacklist = oldBL
		_ = UseWhitelistCache(0, 0)
		_ = UseBlacklistCache(0, 0)
	})
	config.Whitelist = nil
	config.Blacklist = nil
	_ = UseWhitelistCache(0, 0)
	_ = UseBlacklistCache(0, 0)
}

// TestGinIPCacheReuseAndConcurrentReload 验证同参数热重载复用分片缓存但清空旧项, 且并发读写零竞争.
func TestGinIPCacheReuseAndConcurrentReload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGinIPTestState(t)
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
	first.lru.Add("10.1.2.3", true)
	if err := UseWhitelistCache(128, 60); err != nil {
		t.Fatal(err)
	}
	second := whitelistCache.Load()
	if second != first {
		t.Fatal("unchanged capacity/lifetime should reuse ipCache instance")
	}
	if _, ok := second.lru.Get("10.1.2.3"); ok {
		t.Fatal("reused cache should purge entries on reload")
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.RemoteAddr = "10.1.2.3:12345"
		for range 500 {
			_ = WhitelistChecker(c)
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

// TestGinEmptyIPListZeroAllocFastPath 验证空黑白名单跳过 c.ClientIP() 解析且零分配.
func TestGinEmptyIPListZeroAllocFastPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetGinIPTestState(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.RemoteAddr = "198.51.100.10:8080"
	c.Request.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")

	allocs := testing.AllocsPerRun(100, func() {
		if BlacklistChecker(c) {
			t.Fatal("empty blacklist must return false")
		}
		if !WhitelistChecker(c) {
			t.Fatal("empty whitelist must return true")
		}
	})
	if allocs != 0 {
		t.Fatalf("empty IP list check allocs = %v, want 0", allocs)
	}
}
