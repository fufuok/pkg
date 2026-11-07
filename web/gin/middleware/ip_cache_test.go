package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
)

func resetGinIPTestState(t *testing.T) {
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

// TestGinConcurrentLoadConfigAndIPChecker 验证生产模式 LoadConfig 热重载与并发黑白名单请求之间零数据竞争.
func TestGinConcurrentLoadConfigAndIPChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = UseWhitelistCache(0, 0)
	_ = UseBlacklistCache(0, 0)
	t.Cleanup(func() {
		_ = UseWhitelistCache(0, 0)
		_ = UseBlacklistCache(0, 0)
	})

	config.InitTester()
	tempRoot := filepath.Dir(config.RootPath)
	cfgFile := config.ConfigFile
	wlFile := filepath.Join(tempRoot, "etc", "whitelist.conf")
	blFile := filepath.Join(tempRoot, "etc", "blacklist.conf")
	// 切回非 tester 运行态以模拟真实生产 LoadConfig 发布与快照读取
	config.StopTester()

	oldRoot, oldCfgFile := config.RootPath, config.ConfigFile
	oldAppSecret := config.AppBaseSecretValue
	oldLogPath, oldDefLogPath := config.LogPath, config.DefaultLogPath
	oldWLFile, oldBLFile := config.WhitelistConfigFile, config.BlacklistConfigFile
	oldDefWLFile, oldDefBLFile := config.DefaultWhitelistConfigFile, config.DefaultBlacklistConfigFile
	oldEnvPath, oldEnvMain := config.EnvFilePath, config.EnvMainFile
	oldConfPtr := config.Config()
	t.Cleanup(func() {
		config.RootPath, config.ConfigFile = oldRoot, oldCfgFile
		config.AppBaseSecretValue = oldAppSecret
		config.LogPath, config.DefaultLogPath = oldLogPath, oldDefLogPath
		config.WhitelistConfigFile, config.BlacklistConfigFile = oldWLFile, oldBLFile
		config.DefaultWhitelistConfigFile, config.DefaultBlacklistConfigFile = oldDefWLFile, oldDefBLFile
		config.EnvFilePath, config.EnvMainFile = oldEnvPath, oldEnvMain
		_ = oldConfPtr
	})

	testDir := t.TempDir()
	_ = tempRoot
	_ = cfgFile
	etcDir := filepath.Join(testDir, "etc")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wlFile = filepath.Join(etcDir, "whitelist.conf")
	blFile = filepath.Join(etcDir, "blacklist.conf")
	mainFile := filepath.Join(etcDir, "app.json")
	if err := os.WriteFile(wlFile, []byte("10.0.0.0/8\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blFile, []byte("198.51.100.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainJSON := `{"whitelist_conf":{"path":"` + filepath.ToSlash(wlFile) + `"},"blacklist_conf":{"path":"` + filepath.ToSlash(blFile) + `"}}`
	if err := os.WriteFile(mainFile, []byte(mainJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	config.AppBaseSecretValue = "RaceTestSecret"
	config.RootPath = filepath.Join(testDir, "bin")
	config.DefaultLogPath = filepath.Join(testDir, "log")
	config.ConfigFile = mainFile
	config.EnvFilePath = etcDir
	config.EnvMainFile = filepath.Join(etcDir, "app.env")
	config.DefaultWhitelistConfigFile = wlFile
	config.DefaultBlacklistConfigFile = blFile

	if err := config.LoadConfig(); err != nil {
		t.Fatalf("initial LoadConfig: %v", err)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.RemoteAddr = "10.1.2.3:8080"
			for range 200 {
				if !WhitelistChecker(c) {
					t.Error("expected 10.1.2.3 to be whitelisted")
					return
				}
				if BlacklistChecker(c) {
					t.Error("expected 10.1.2.3 not to be blacklisted")
					return
				}
			}
		})
	}
	for range 20 {
		if err := config.LoadConfig(); err != nil {
			t.Fatalf("concurrent LoadConfig: %v", err)
		}
	}
	wg.Wait()
}
