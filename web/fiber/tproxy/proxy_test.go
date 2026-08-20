package tproxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fufuok/ants"
	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/xhash"
)

func TestMain(m *testing.M) {
	code := m.Run()
	_ = ants.ReleaseTimeout(5 * time.Second)
	os.Exit(code)
}

func testXToken() string {
	xip := "118.118.8.8"
	xtime := "2024-04-10T14:11:00+08:00"
	tokenSalt := "test-salt"
	xtoken := xhash.HashString(xip, xtime, tokenSalt)
	return xtoken
}

func TestSetClientIP(t *testing.T) {
	xtoken := testXToken()
	assert.Equal(t, "13132673241273045767", xtoken)
}

// TestGetClientIPAcceptsFreshProxyToken 近时 RFC3339 + 正确 FNV 令牌应被接受.
// 算法仍是 HashString(xip, xtime, WebTokenSalt), 本用例同时锁住该输出契约.
func TestGetClientIPAcceptsFreshProxyToken(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	xtime := common.GTimeNowString(time.RFC3339)
	xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)

	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    xtoken,
		HeaderXProxyTime:     xtime,
	})
	if got != xip {
		t.Fatalf("GetClientIP() = %q, want header IP %q", got, xip)
	}
}

// TestGetClientIPRejectsExpiredProxyToken 过期但哈希正确的代理头必须回退 RemoteIP.
// 令牌算法仍是 HashString, 2024-04-10 的固定输出保持 13132673241273045767.
func TestGetClientIPRejectsExpiredProxyToken(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	xtime := "2024-04-10T14:11:00+08:00"
	xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)
	assert.Equal(t, "13132673241273045767", xtoken)

	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    xtoken,
		HeaderXProxyTime:     xtime,
	})
	remoteIP := getClientIPFromRequest(t, nil)
	if got == xip {
		t.Fatalf("expired proxy token accepted, GetClientIP() = %q", got)
	}
	if got != remoteIP {
		t.Fatalf("GetClientIP() = %q, want fallback %q", got, remoteIP)
	}
}

// TestGetClientIPRejectsFutureProxyToken 超出 SignTTL 的未来时间即使哈希正确也必须拒绝.
func TestGetClientIPRejectsFutureProxyToken(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	xtime := common.GTimeNow().Add(time.Duration(config.WebSignTTLDefault+1) * time.Second).Format(time.RFC3339)
	xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)

	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    xtoken,
		HeaderXProxyTime:     xtime,
	})
	if got == xip {
		t.Fatalf("future proxy token accepted, GetClientIP() = %q", got)
	}
}

// TestGetClientIPRejectsFarFutureProxyToken 年份 9999 即使哈希正确也不能绕过 SignTTL 窗口.
func TestGetClientIPRejectsFarFutureProxyToken(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	xtime := "9999-12-31T23:59:59Z"
	xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)

	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    xtoken,
		HeaderXProxyTime:     xtime,
	})
	if got == xip {
		t.Fatalf("year 9999 proxy token accepted, GetClientIP() = %q", got)
	}
}

// TestGetClientIPRejectsInvalidProxyTime 非法 RFC3339 即使按原文算出令牌也不能信头.
func TestGetClientIPRejectsInvalidProxyTime(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	xtime := "not-rfc3339"
	xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)

	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    xtoken,
		HeaderXProxyTime:     xtime,
	})
	if got == xip {
		t.Fatalf("invalid proxy time accepted, GetClientIP() = %q", got)
	}
}

// TestGetClientIPRejectsWrongToken 令牌与 HashString 不一致时回退 RemoteIP.
func TestGetClientIPRejectsWrongToken(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	got := getClientIPFromRequest(t, map[string]string{
		HeaderXProxyClientIP: xip,
		HeaderXProxyToken:    "0",
		HeaderXProxyTime:     common.GTimeNowString(time.RFC3339),
	})
	remoteIP := getClientIPFromRequest(t, nil)
	if got != remoteIP {
		t.Fatalf("GetClientIP() = %q, want fallback %q", got, remoteIP)
	}
}

// TestValidProxyClientIPWindow 直接覆盖令牌窗口, 不依赖 Fiber RemoteAddr 解析.
func TestValidProxyClientIPWindow(t *testing.T) {
	restoreWebTokenSalt(t, "test-salt")

	xip := "118.118.8.8"
	freshTime := common.GTimeNowString(time.RFC3339)
	freshToken := xhash.HashString(xip, freshTime, config.WebTokenSalt)
	if !validProxyClientIP(xip, freshToken, freshTime) {
		t.Fatal("fresh HashString token should be accepted")
	}

	expiredTime := "2024-04-10T14:11:00+08:00"
	expiredToken := xhash.HashString(xip, expiredTime, config.WebTokenSalt)
	assert.Equal(t, "13132673241273045767", expiredToken)
	if validProxyClientIP(xip, expiredToken, expiredTime) {
		t.Fatal("expired HashString token should be rejected")
	}

	futureTime := common.GTimeNow().Add(time.Duration(config.WebSignTTLDefault+1) * time.Second).Format(time.RFC3339)
	futureToken := xhash.HashString(xip, futureTime, config.WebTokenSalt)
	if validProxyClientIP(xip, futureToken, futureTime) {
		t.Fatal("future HashString token should be rejected")
	}

	// 年份 9999 会让 Time.Sub 饱和为 MinInt64, 取绝对值后仍为负, 旧实现会误判在窗口内.
	farFutureTime := "9999-12-31T23:59:59Z"
	farFutureToken := xhash.HashString(xip, farFutureTime, config.WebTokenSalt)
	if validProxyClientIP(xip, farFutureToken, farFutureTime) {
		t.Fatal("year 9999 proxy time should be rejected")
	}

	if validProxyClientIP(xip, xhash.HashString(xip, "not-rfc3339", config.WebTokenSalt), "not-rfc3339") {
		t.Fatal("invalid RFC3339 time should be rejected")
	}
	if validProxyClientIP(xip, "0", freshTime) {
		t.Fatal("wrong token should be rejected")
	}
}

// restoreWebTokenSalt 设置测试盐并在结束时恢复, 避免污染包级 WebTokenSalt.
func restoreWebTokenSalt(t *testing.T, salt string) {
	t.Helper()
	old := config.WebTokenSalt
	config.WebTokenSalt = salt
	t.Cleanup(func() {
		config.WebTokenSalt = old
	})
}

// getClientIPFromRequest 用独立 Fiber 请求读取 GetClientIP, 避免 Locals 跨用例串扰.
func getClientIPFromRequest(t *testing.T, headers map[string]string) string {
	t.Helper()
	var got string
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		got = GetClientIP(c)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	_ = resp.Body.Close()
	return got
}

func BenchmarkSetClientIP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = testXToken()
	}
}

// go test -run=nil -benchmem -bench=BenchmarkSetClientIP
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/web/fiber/tproxy
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// BenchmarkSetClientIP-16         10461621               115.2 ns/op            72 B/op          2 allocs/op
