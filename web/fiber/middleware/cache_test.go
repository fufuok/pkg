package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/timeout"
)

// TestFiberCacheRequestIdentity 验证请求键保留完整 URI, Host 和表示相关请求头.
func TestFiberCacheRequestIdentity(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	calls := 0
	app.Get("/*", func(c fiber.Ctx) error {
		calls++
		return c.SendString(strconv.Itoa(calls))
	})
	cases := []struct {
		target string
		header http.Header
		want   string
	}{
		{"http://one.test/a?q=1", nil, "1"},
		{"http://one.test/a?q=1", nil, "1"},
		{"http://one.test/a?q=2", nil, "2"},
		{"http://one.test/b?q=1", nil, "3"},
		{"http://two.test/a?q=1", nil, "4"},
		{"http://one.test/a?q=1", http.Header{"Accept-Language": {"zh-CN"}}, "5"},
		{"http://one.test/a?q=1", http.Header{"Accept-Language": {"en"}}, "6"},
		{"http://one.test/a?q=1", http.Header{"Accept-Encoding": {"gzip"}}, "7"},
		{"http://one.test/a?q=1", http.Header{"Accept-Encoding": {"gzip"}}, "7"},
	}
	for _, tc := range cases {
		body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, tc.target, "", tc.header)
		if string(body) != tc.want {
			t.Fatalf("request %s %v: body = %q, want %q", tc.target, tc.header, body, tc.want)
		}
	}
}

// TestFiberCacheRawAndEffectiveHost 验证可信代理下原始 Host 和实际业务 Host 都参与缓存键.
func TestFiberCacheRawAndEffectiveHost(t *testing.T) {
	app := fiber.New(fiber.Config{TrustProxy: true, TrustProxyConfig: fiber.TrustProxyConfig{Loopback: true}})
	app.Use(func(c fiber.Ctx) error {
		c.RequestCtx().SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
		return c.Next()
	})
	app.Use(CacheByRequestURI(time.Minute))
	calls := 0
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		return c.SendString(fmt.Sprintf("%d:%s:%s", calls, c.Request().Header.Host(), c.Host()))
	})
	for _, tc := range []struct{ raw, effective, want string }{
		{"one.test", "shared.test", "1:one.test:shared.test"},
		{"two.test", "shared.test", "2:two.test:shared.test"},
		{"one.test", "other.test", "3:one.test:other.test"},
		{"one.test", "shared.test", "1:one.test:shared.test"},
	} {
		body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://"+tc.raw+"/", "", http.Header{"X-Forwarded-Host": {tc.effective}})
		if string(body) != tc.want {
			t.Fatalf("raw/effective host = %s/%s: body = %q, want %q", tc.raw, tc.effective, body, tc.want)
		}
	}
}

// TestFiberCacheHeaderCardinality 验证头字段单行逗号值与多行值不会复用彼此的响应.
func TestFiberCacheHeaderCardinality(t *testing.T) {
	for _, name := range []string{"X-Tenant", "Content-Type"} {
		t.Run(name, func(t *testing.T) {
			app := fiber.New()
			app.Use(CacheByRequestURI(time.Minute, CacheConfig{Vary: []string{"X-Tenant"}}))
			app.Get("/", func(c fiber.Ctx) error { return c.SendString(c.Get(name)) })
			multipleValue := "a"
			if name == "Content-Type" {
				// fasthttp 对 Content-Type 使用单值专用槽, 重复行以最后一项作为业务可见值.
				multipleValue = "b"
			}
			for _, tc := range []struct {
				values []string
				want   string
			}{
				{[]string{"a,b"}, "a,b"},
				{[]string{"a", "b"}, multipleValue},
				{[]string{"a,b"}, "a,b"},
			} {
				body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{name: tc.values})
				if string(body) != tc.want {
					t.Fatalf("%s = %q: body = %q, want %q", name, tc.values, body, tc.want)
				}
			}
		})
	}
}

// TestFiberCacheUpstreamResponseState 验证已有状态, 正文, 响应流和 SkipBody 不会被缓存命中覆盖.
func TestFiberCacheUpstreamResponseState(t *testing.T) {
	for _, mode := range []string{"status", "body", "stream", "skip_body"} {
		t.Run(mode, func(t *testing.T) {
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				if c.Get("X-Override") != "" {
					switch mode {
					case "status":
						c.Status(fiber.StatusForbidden)
					case "body":
						c.Response().SetBodyString("prefix:")
					case "stream":
						if err := c.SendStream(strings.NewReader("stream"), len("stream")); err != nil {
							return err
						}
					case "skip_body":
						c.Response().SkipBody = true
					}
				}
				return c.Next()
			})
			app.Use(CacheByRequestURI(time.Minute))
			calls := 0
			app.Get("/", func(c fiber.Ctx) error {
				calls++
				if !c.Response().IsBodyStream() {
					c.Response().AppendBodyString(strconv.Itoa(calls))
				}
				return nil
			})
			fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
			req := httptest.NewRequest(fiber.MethodGet, "http://example.test/", nil)
			req.Header.Set("X-Override", "yes")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(resp.Body)
			if err := resp.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if readErr != nil && !(mode == "skip_body" && errors.Is(readErr, io.ErrUnexpectedEOF)) {
				t.Fatal(readErr)
			}
			if calls != 2 {
				t.Fatalf("upstream %s handler calls = %d, want 2", mode, calls)
			}
			if mode == "status" && resp.StatusCode != fiber.StatusForbidden ||
				mode == "body" && string(body) != "prefix:2" || mode == "stream" && string(body) != "stream" ||
				mode == "skip_body" && len(body) != 0 {
				t.Fatalf("upstream %s: status = %d, body = %q", mode, resp.StatusCode, body)
			}
		})
	}
}

// TestFiberCacheRejectMultipleConfigs 验证无意传入多份配置会在注册阶段报错, 不静默丢弃配置.
func TestFiberCacheRejectMultipleConfigs(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("multiple cache configurations must panic")
		}
	}()
	CacheByRequestURI(time.Minute, CacheConfig{}, CacheConfig{})
}

// TestFiberCacheRequestBypass 验证有凭据, 条件, 禁缓存或不匹配方法的请求不会命中或存入缓存.
func TestFiberCacheRequestBypass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		body   string
		header http.Header
	}{
		{"cookie", fiber.MethodGet, "", http.Header{"Cookie": {"uid=alice"}}},
		{"authorization", fiber.MethodGet, "", http.Header{"Authorization": {"Bearer token"}}},
		{"authorization_second_line", fiber.MethodGet, "", http.Header{"Authorization": {"", "Bearer token"}}},
		{"cookie_second_line", fiber.MethodGet, "", http.Header{"Cookie": {"", "uid=alice"}}},
		{"no_store", fiber.MethodGet, "", http.Header{"Cache-Control": {"no-store"}}},
		{"no_cache", fiber.MethodGet, "", http.Header{"Cache-Control": {"no-cache"}}},
		{"pragma", fiber.MethodGet, "", http.Header{"Pragma": {"no-cache"}}},
		{"range", fiber.MethodGet, "", http.Header{"Range": {"bytes=0-1"}}},
		{"conditional", fiber.MethodGet, "", http.Header{"If-None-Match": {"\"old\""}}},
		{"event_stream", fiber.MethodGet, "", http.Header{"Accept": {"text/event-stream"}}},
		{"get_body", fiber.MethodGet, "body", nil},
		{"post", fiber.MethodPost, "body", nil},
		{"head", fiber.MethodHead, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(CacheByRequestURI(time.Minute))
			calls := 0
			app.All("/", func(c fiber.Ctx) error {
				calls++
				return c.SendString(strconv.Itoa(calls))
			})
			for range 2 {
				fiberCacheTestRequest(t, app, tc.method, "http://example.test/", tc.body, tc.header)
			}
			if calls != 2 {
				t.Fatalf("handler calls = %d, want 2", calls)
			}
		})
	}
}

// TestFiberCacheRawData 验证只读 POST 同时按正文, 路径, 查询, 媒体类型和编码分区, 正文仍供业务读取.
func TestFiberCacheRawData(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRawData(time.Minute, CacheConfig{MaxRequestBodyBytes: 8}))
	calls := 0
	app.All("/*", func(c fiber.Ctx) error {
		calls++
		return c.SendString(fmt.Sprintf("%d:%s", calls, c.Request().Body()))
	})
	cases := []struct {
		method string
		target string
		body   string
		header http.Header
		want   string
	}{
		{fiber.MethodPost, "/a?q=1", "same", nil, "1:same"},
		{fiber.MethodPost, "/a?q=1", "same", nil, "1:same"},
		{fiber.MethodPost, "/a?q=1", "new", nil, "2:new"},
		{fiber.MethodPost, "/b?q=1", "same", nil, "3:same"},
		{fiber.MethodPost, "/a?q=2", "same", nil, "4:same"},
		{fiber.MethodPost, "/a?q=1", "same", http.Header{"Content-Type": {"application/json"}}, "5:same"},
		{fiber.MethodPost, "/a?q=1", "same", http.Header{"Content-Encoding": {"gzip"}}, "6:same"},
		{fiber.MethodPost, "/a?q=1", "123456789", nil, "7:123456789"},
		{fiber.MethodPost, "/a?q=1", "123456789", nil, "8:123456789"},
		{fiber.MethodGet, "/a?q=1", "same", nil, "9:same"},
		{fiber.MethodGet, "/a?q=1", "same", nil, "10:same"},
	}
	for _, tc := range cases {
		body, _ := fiberCacheTestRequest(t, app, tc.method, "http://example.test"+tc.target, tc.body, tc.header)
		if string(body) != tc.want {
			t.Fatalf("request %s %s %v: body = %q, want %q", tc.method, tc.target, tc.header, body, tc.want)
		}
	}
}

// TestFiberCacheResponseBypass 覆盖隐私响应, 禁缓存指令, 错误, 流式媒体和超大正文的准入边界.
func TestFiberCacheResponseBypass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header string
		value  string
		body   string
	}{
		{"private", 200, "Cache-Control", "private", "body"},
		{"private_field", 200, "Cache-Control", `private="X-Secret"`, "body"},
		{"no_store", 200, "Cache-Control", "no-store", "body"},
		{"no_cache", 200, "Cache-Control", "no-cache", "body"},
		{"max_age_zero", 200, "Cache-Control", "max-age=0", "body"},
		{"cookie", 200, "Set-Cookie", "uid=alice", "body"},
		{"vary_star", 200, "Vary", "*", "body"},
		{"vary_unknown", 200, "Vary", "X-Unknown", "body"},
		{"event_stream", 200, "Content-Type", "text/event-stream", "body"},
		{"partial", 206, "", "", "body"},
		{"redirect", 302, "Location", "/other", "body"},
		{"error", 500, "", "", "body"},
		{"oversize", 200, "", "", "123456789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(CacheByRequestURI(time.Minute, CacheConfig{MaxBodyBytes: 8}))
			calls := 0
			app.Get("/", func(c fiber.Ctx) error {
				calls++
				if tc.header != "" {
					c.Set(tc.header, tc.value)
				}
				c.Status(tc.status)
				return c.SendString(tc.body)
			})
			for range 2 {
				fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
			}
			if calls != 2 {
				t.Fatalf("handler calls = %d, want 2", calls)
			}
		})
	}
}

// TestFiberCacheUpstreamPrivacy 验证上游逐请求策略能够禁止命中此前已经存入的公开响应.
func TestFiberCacheUpstreamPrivacy(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"Cache-Control", "private"},
		{"Set-Cookie", "uid=alice"},
		{"Vary", "X-Unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				if c.Get("X-Private") != "" {
					c.Set(tc.name, tc.value)
				}
				return c.Next()
			})
			app.Use(CacheByRequestURI(time.Minute))
			calls := 0
			app.Get("/", func(c fiber.Ctx) error {
				calls++
				return c.SendString(strconv.Itoa(calls))
			})
			fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
			body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"X-Private": {"yes"}})
			if string(body) != "2" {
				t.Fatalf("private request received cached body %q", body)
			}
		})
	}
}

// TestFiberCacheDynamicHeadersAndBodyOwnership 验证命中保留本次追踪/CORS 头及业务元数据, 不复用被外层改写的正文.
func TestFiberCacheDynamicHeadersAndBodyOwnership(t *testing.T) {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Request-ID", c.Get("X-Request-ID"))
		c.Set("Access-Control-Allow-Origin", c.Get("Origin"))
		if err := c.Next(); err != nil {
			return err
		}
		if c.Get("X-Mutate") != "" {
			c.Response().Body()[0] = 'X'
		}
		return nil
	})
	app.Use(CacheByRequestURI(time.Minute))
	calls := 0
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		c.Set("X-Backend-ID", "first")
		c.Type("json")
		c.Set("ETag", `"fixed"`)
		return c.SendString("cached")
	})
	fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"Origin": {"https://client.test"}})
	_, header := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{
		"X-Request-Id": {"second"}, "Origin": {"https://client.test"}, "X-Mutate": {"yes"},
	})
	if header.Get("X-Request-ID") != "second" || header.Get("Access-Control-Allow-Origin") != "https://client.test" ||
		header.Get("X-Backend-ID") != "first" || header.Get("ETag") != `"fixed"` || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected replay headers: %v", header)
	}
	body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"Origin": {"https://client.test"}})
	if string(body) != "cached" || calls != 1 {
		t.Fatalf("cache ownership: body = %q, handler calls = %d", body, calls)
	}
}

// TestFiberCacheCompressionOrders 验证压缩位于缓存内外两种顺序时, 压缩和原始表示均可安全命中.
func TestFiberCacheCompressionOrders(t *testing.T) {
	for _, compressionFirst := range []bool{true, false} {
		t.Run(strconv.FormatBool(compressionFirst), func(t *testing.T) {
			app := fiber.New()
			if compressionFirst {
				app.Use(compress.New(), CacheByRequestURI(time.Minute))
			} else {
				app.Use(CacheByRequestURI(time.Minute), compress.New())
			}
			calls := 0
			want := strings.Repeat("cache response ", 100)
			app.Get("/", func(c fiber.Ctx) error {
				calls++
				return c.SendString(want)
			})
			for _, encoding := range []string{"gzip", "", "gzip", ""} {
				body, header := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"Accept-Encoding": {encoding}})
				if encoding == "gzip" {
					if header.Get("Content-Encoding") != "gzip" {
						t.Fatalf("content encoding = %q, want gzip", header.Get("Content-Encoding"))
					}
					reader, err := gzip.NewReader(bytes.NewReader(body))
					if err != nil {
						t.Fatal(err)
					}
					body, err = io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					if err := reader.Close(); err != nil {
						t.Fatal(err)
					}
				} else if header.Get("Content-Encoding") != "" {
					t.Fatalf("plain request received content encoding %q", header.Get("Content-Encoding"))
				}
				if string(body) != want {
					t.Fatalf("decoded response differs: %q", body)
				}
			}
			if calls != 2 {
				t.Fatalf("handler calls = %d, want 2", calls)
			}
		})
	}
}

// TestFiberCacheStreams 验证缓存层不消费请求或响应流, SendFile 和 SSE 始终由原处理链执行.
func TestFiberCacheStreams(t *testing.T) {
	for _, mode := range []string{"response", "sse", "file", "request"} {
		t.Run(mode, func(t *testing.T) {
			app := fiber.New()
			filename := filepath.Join(t.TempDir(), "cached.txt")
			if err := os.WriteFile(filename, []byte("stream body"), 0o600); err != nil {
				t.Fatal(err)
			}
			app.Use(func(c fiber.Ctx) error {
				if mode == "request" {
					c.Request().SetBodyStream(strings.NewReader("stream body"), -1)
				}
				err := c.Next()
				if mode != "request" && !c.Response().IsBodyStream() {
					t.Error("cache consumed the response stream")
				}
				return err
			})
			method := fiber.MethodGet
			if mode == "request" {
				method = fiber.MethodPost
				app.Use(CacheByRawData(time.Minute))
			} else {
				app.Use(CacheByRequestURI(time.Minute))
			}
			calls := 0
			app.All("/", func(c fiber.Ctx) error {
				calls++
				if mode == "request" {
					if !c.Request().IsBodyStream() {
						t.Error("cache consumed the request stream")
					}
					body, err := io.ReadAll(c.Request().BodyStream())
					if err != nil {
						return err
					}
					return c.Send(body)
				}
				if mode == "file" {
					return c.SendFile(filename)
				}
				if mode == "sse" {
					c.Set("Content-Type", "text/event-stream")
				}
				return c.SendStream(strings.NewReader("stream body"))
			})
			for range 2 {
				body, _ := fiberCacheTestRequest(t, app, method, "http://example.test/", "", nil)
				if string(body) != "stream body" {
					t.Fatalf("stream body = %q", body)
				}
			}
			if calls != 2 {
				t.Fatalf("handler calls = %d, want 2", calls)
			}
		})
	}
}

// TestFiberCacheConcurrentAdmission 验证同键冷请求只共享已经准入的响应, 禁缓存结果各自执行业务.
func TestFiberCacheConcurrentAdmission(t *testing.T) {
	for _, cacheable := range []bool{true, false} {
		t.Run(strconv.FormatBool(cacheable), func(t *testing.T) {
			app := fiber.New()
			app.Use(CacheByRequestURI(time.Minute))
			var calls atomic.Int32
			app.Get("/", func(c fiber.Ctx) error {
				calls.Add(1)
				if !cacheable {
					c.Set("Cache-Control", "no-store")
					return c.SendString(c.Get("X-Request-ID"))
				}
				return c.SendString("shared")
			})
			const requests = 20
			start := make(chan struct{})
			failures := make(chan error, requests)
			var workers sync.WaitGroup
			for i := range requests {
				workers.Go(func() {
					<-start
					id := strconv.Itoa(i)
					req := httptest.NewRequest(fiber.MethodGet, "http://example.test/", nil)
					req.Header.Set("X-Request-ID", id)
					resp, err := app.Test(req)
					if err != nil {
						failures <- err
						return
					}
					body, readErr := io.ReadAll(resp.Body)
					closeErr := resp.Body.Close()
					want := "shared"
					if !cacheable {
						want = id
					}
					if readErr != nil || closeErr != nil || string(body) != want {
						failures <- fmt.Errorf("body = %q, want %q, read = %v, close = %v", body, want, readErr, closeErr)
					}
				})
			}
			close(start)
			workers.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
			wantCalls := int32(requests)
			if cacheable {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("handler calls = %d, want %d", calls.Load(), wantCalls)
			}
		})
	}
}

// TestFiberCacheConfigAndExpiry 验证配置确实传入共享存储, LRU 逐出和绝对 TTL 不会变成永久缓存.
func TestFiberCacheConfigAndExpiry(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(20*time.Millisecond, CacheConfig{Capacity: 1}))
	calls := 0
	app.Get("/*", func(c fiber.Ctx) error {
		calls++
		return c.SendString(strconv.Itoa(calls))
	})
	for _, path := range []string{"/a", "/b", "/a"} {
		fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test"+path, "", nil)
	}
	if calls != 3 {
		t.Fatalf("capacity-one handler calls = %d, want 3", calls)
	}
	time.Sleep(30 * time.Millisecond)
	body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/a", "", nil)
	if string(body) != "4" {
		t.Fatalf("expired body = %q, want 4", body)
	}
}

// TestFiberCacheNormalizedCredential 验证网络输入的小写凭据由默认框架规范化后阻止缓存.
func TestFiberCacheNormalizedCredential(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	calls := 0
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		return c.SendString(strconv.Itoa(calls))
	})
	for range 2 {
		fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"authorization": {"Bearer token"}})
	}
	if calls != 2 {
		t.Fatalf("lowercase credential handler calls = %d, want 2", calls)
	}
}

// TestFiberCacheNonNormalizedHeaders 关闭头名规范化后, 小写凭据与小写 Vary 维度仍须识别:
// 凭据请求不缓存, 不同租户不串用. 适配器只收集相关请求头, 名称匹配必须大小写无关.
func TestFiberCacheNonNormalizedHeaders(t *testing.T) {
	app := fiber.New(fiber.Config{DisableHeaderNormalizing: true})
	app.Use(CacheByRequestURI(time.Minute, CacheConfig{Vary: []string{"X-Tenant"}}))
	calls := 0
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		return c.SendString(strconv.Itoa(calls))
	})
	for range 2 {
		fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"authorization": {"Bearer token"}})
	}
	if calls != 2 {
		t.Fatalf("lowercase credential handler calls = %d, want 2", calls)
	}
	a, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"x-tenant": {"a"}})
	b, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"x-tenant": {"b"}})
	if string(a) == string(b) {
		t.Fatalf("lowercase Vary header did not partition: %q", a)
	}
}

// TestFiberCacheIgnoresUnkeyedHeaders 验证不参与策略和键的请求头 (如 User-Agent) 不影响命中.
func TestFiberCacheIgnoresUnkeyedHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	calls := 0
	app.Get("/", func(c fiber.Ctx) error {
		calls++
		return c.SendString(strconv.Itoa(calls))
	})
	for _, agent := range []string{"agent-a", "agent-b"} {
		fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", http.Header{"User-Agent": {agent}})
	}
	if calls != 1 {
		t.Fatalf("unkeyed header partitioned the cache: handler calls = %d", calls)
	}
}

// TestFiberCacheReentry 覆盖同一请求再次经过缓存: RestartRouting 会重新执行全局中间件,
// 同一实例也可能在路由组和路由上重复注册. 内层不得等待外层持有的键锁而自锁 (app.Test 超时即失败),
// 实际执行的路由仍可正常缓存和回放.
func TestFiberCacheReentry(t *testing.T) {
	t.Run("restart_routing", func(t *testing.T) {
		app := fiber.New()
		var calls atomic.Int32
		app.Use(CacheByRequestURI(time.Minute))
		app.Get("/a", func(c fiber.Ctx) error {
			c.Path("/b")
			return c.RestartRouting()
		})
		app.Get("/b", func(c fiber.Ctx) error {
			calls.Add(1)
			return c.SendString("hello-b")
		})
		for i := range 2 {
			if body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/a", "", nil); string(body) != "hello-b" {
				t.Fatalf("request %d body = %q", i, body)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("rerouted response was not cached: handler calls = %d", calls.Load())
		}
	})
	t.Run("double_registration", func(t *testing.T) {
		app := fiber.New()
		cache := CacheByRequestURI(time.Minute)
		var calls atomic.Int32
		app.Group("/api", cache).Get("/x", cache, func(c fiber.Ctx) error {
			calls.Add(1)
			return c.SendString("x")
		})
		for i := range 2 {
			if body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/api/x", "", nil); string(body) != "x" {
				t.Fatalf("request %d body = %q", i, body)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", calls.Load())
		}
	})
}

// TestFiberCacheOuterConditionalHeaders 验证外层在缓存之前按未进入键的输入设置的头不进入条目:
// 命中时外层会为当前请求重新设置, 存入条目只会把某个访客的值回放给其他访客. 业务设置的头照常回放.
func TestFiberCacheOuterConditionalHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if strings.Contains(c.Get("User-Agent"), "internal") {
			c.Set("X-Debug-Backend", "10.0.0.5")
		}
		c.Set("X-Frame-Options", "DENY")
		return c.Next()
	}, CacheByRequestURI(time.Minute))
	app.Get("/p", func(c fiber.Ctx) error {
		c.Set("X-Page", "1")
		return c.SendString("page")
	})
	_, first := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/p", "", http.Header{"User-Agent": {"internal-probe"}})
	if first.Get("X-Debug-Backend") != "10.0.0.5" {
		t.Fatalf("outer header missing on miss: %v", first)
	}
	for _, agent := range []string{"Mozilla/5.0", "internal-probe"} {
		body, hit := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/p", "", http.Header{"User-Agent": {agent}})
		want := map[string]string{"Mozilla/5.0": "", "internal-probe": "10.0.0.5"}[agent]
		if hit.Get("Age") == "" || string(body) != "page" || hit.Get("X-Debug-Backend") != want ||
			hit.Get("X-Frame-Options") != "DENY" || hit.Get("X-Page") != "1" {
			t.Fatalf("agent %s hit headers = %v, body = %q", agent, hit, body)
		}
	}
}

// TestFiberCacheEffectiveRoute 验证外层按未进入键的请求头改写路径或查询时, 不同改写结果分别缓存:
// 键只含原始请求目标时, 移动端重写一类的中间件会让桌面与移动响应互相覆盖.
func TestFiberCacheEffectiveRoute(t *testing.T) {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		switch agent := c.Get("User-Agent"); {
		case strings.Contains(agent, "Mobile"):
			c.Path("/m" + c.Path())
		case strings.Contains(agent, "Lite"):
			c.Request().URI().SetQueryString("lite=1")
		}
		return c.Next()
	}, CacheByRequestURI(time.Minute))
	page := func(c fiber.Ctx) error {
		return c.SendString(c.Path() + "?" + string(c.Request().URI().QueryString()))
	}
	app.Get("/home", page)
	app.Get("/m/home", page)
	for range 2 {
		for agent, want := range map[string]string{"Desktop": "/home?", "Mobile": "/m/home?", "Lite": "/home?lite=1"} {
			body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/home", "", http.Header{"User-Agent": {agent}})
			if string(body) != want {
				t.Fatalf("agent %s body = %q, want %q", agent, body, want)
			}
		}
	}
}

// TestFiberCacheErrorIdentity 验证业务错误保留原对象并交还 ErrorHandler, 错误响应不复用于后续请求.
func TestFiberCacheErrorIdentity(t *testing.T) {
	want := errors.New("backend unavailable")
	errorCalls := 0
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		errorCalls++
		if err != want {
			t.Errorf("error identity = %v, want original error", err)
		}
		return c.Status(fiber.StatusBadGateway).SendString("retry")
	}})
	app.Use(CacheByRequestURI(time.Minute))
	app.Get("/", func(fiber.Ctx) error { return want })
	for range 2 {
		body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
		if string(body) != "retry" {
			t.Fatalf("error response = %q, want retry", body)
		}
	}
	if errorCalls != 2 {
		t.Fatalf("error handler calls = %d, want 2", errorCalls)
	}
}

// TestFiberCacheTimeoutDoesNotReadActiveResponse 验证真实超时直接返回原错误, 活动 worker 的响应不会入缓存.
func TestFiberCacheTimeoutDoesNotReadActiveResponse(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute), WithTimeout(time.Millisecond))
	release := make(chan struct{})
	done := make(chan struct{})
	app.Get("/", func(c fiber.Ctx) error {
		defer close(done)
		ctx := c.Context()
		<-ctx.Done()
		<-release
		return ctx.Err()
	})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() {
		unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("timed-out worker did not exit")
		}
	})
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "http://example.test/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("timeout status = %d, want 503", resp.StatusCode)
	}
}

// TestFiberCacheInsideTimeout 覆盖 `WithTimeout -> 缓存 -> 业务`: 缓存运行在超时工作协程, 截止后与请求协程并发访问 Ctx,
// 只能在入口识别 WithTimeout 标记后整段透传. 期限内完成或截止后返回 (错误与 nil) 都不命中, 不存储,
// 也不读取超时协程无锁写入的 fasthttp 标记 (-race 下可复现); 工作协程都能退出.
func TestFiberCacheInsideTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		handler func(c fiber.Ctx) error
	}{
		{"in_time", time.Minute, func(c fiber.Ctx) error { return c.SendString("ok") }},
		{"late_error", time.Millisecond, func(c fiber.Ctx) error { <-c.Context().Done(); return c.Context().Err() }},
		{"late_nil", time.Millisecond, func(c fiber.Ctx) error { <-c.Context().Done(); return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			cache := CacheByRequestURI(time.Minute)
			var finished chan struct{}
			var calls atomic.Int32
			app.Use(WithTimeout(tc.timeout), func(c fiber.Ctx) error {
				defer close(finished)
				return cache(c)
			})
			app.Get("/", func(c fiber.Ctx) error {
				calls.Add(1)
				return tc.handler(c)
			})
			for range 10 {
				finished = make(chan struct{})
				resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "http://example.test/", nil))
				if err != nil {
					t.Fatal(err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatal(err)
				}
				if resp.Header.Get("Age") != "" {
					t.Fatal("cache inside WithTimeout served a hit")
				}
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("timeout worker did not exit")
				}
			}
			if calls.Load() != 10 {
				t.Fatalf("cache inside WithTimeout stored a response: handler calls = %d", calls.Load())
			}
		})
	}
}

// TestFiberCacheSyncDeadlineOuter 覆盖外层同步期限中间件 -> 缓存 -> timeout (OnTimeout 写出 200 兜底并返回 nil):
// 普通 context 期限不代表缓存运行在工作协程, 缓存仍在请求协程以超时标记识别截止, 兜底不缓存;
// 期限内正常完成的响应照常缓存. 截止后的业务只等待取消而不写响应, 避免与 OnTimeout 并发写同一响应
// (这是 Fiber timeout 自身的并发约束, 与缓存无关).
func TestFiberCacheSyncDeadlineOuter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		late  bool
		calls int32
	}{{"timeout_fallback", true, 3}, {"in_time", false, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var calls atomic.Int32
			// 每个工作协程退出时发送一次; 用通道而非 WaitGroup, 工作协程内 Add 与测试协程 Wait 之间没有先后关系.
			exited := make(chan struct{}, 3)
			app.Use(func(c fiber.Ctx) error {
				ctx, cancel := context.WithTimeout(c.Context(), time.Minute)
				defer cancel()
				c.SetContext(ctx)
				return c.Next()
			}, CacheByRequestURI(time.Minute), timeout.New(func(c fiber.Ctx) error {
				defer func() { exited <- struct{}{} }()
				return c.Next()
			}, timeout.Config{
				Timeout:   10 * time.Millisecond,
				OnTimeout: func(c fiber.Ctx) error { return c.SendString("fallback") },
			}))
			app.Get("/", func(c fiber.Ctx) error {
				calls.Add(1)
				if tc.late {
					<-c.Context().Done()
					return nil
				}
				return c.SendString("ok")
			})
			for range 3 {
				fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
			}
			// 命中不进入 timeout, 只等待实际启动的工作协程.
			for range calls.Load() {
				select {
				case <-exited:
				case <-time.After(5 * time.Second):
					t.Fatal("timeout worker did not exit")
				}
			}
			if calls.Load() != tc.calls {
				t.Fatalf("handler calls = %d, want %d", calls.Load(), tc.calls)
			}
		})
	}
}

// TestFiberCacheTimeoutFallbackNotCached 覆盖 `缓存 -> timeout -> 业务` 且 OnTimeout 写出 200 兜底并返回 nil:
// 错误与入口上下文都看不出截止, 只能由同一请求协程写入的 fasthttp 超时标记识别, 兜底响应不得缓存.
func TestFiberCacheTimeoutFallbackNotCached(t *testing.T) {
	app := fiber.New()
	var calls atomic.Int32
	app.Use(CacheByRequestURI(time.Minute), timeout.New(func(c fiber.Ctx) error { return c.Next() }, timeout.Config{
		Timeout:   time.Millisecond,
		OnTimeout: func(c fiber.Ctx) error { return c.SendString("fallback") },
	}))
	app.Get("/", func(c fiber.Ctx) error {
		calls.Add(1)
		<-c.Context().Done()
		return nil
	})
	for i := range 2 {
		if body, _ := fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil); string(body) != "fallback" {
			t.Fatalf("request %d body = %q", i, body)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("timeout fallback was cached: handler calls = %d", calls.Load())
	}
}

// TestFiberCacheInnerContextCancel 验证内层中间件替换并在返回时 cancel 的派生上下文不被当作截止, 响应照常缓存.
func TestFiberCacheInnerContextCancel(t *testing.T) {
	app := fiber.New()
	var calls atomic.Int32
	app.Use(CacheByRequestURI(time.Minute), func(c fiber.Ctx) error {
		ctx, cancel := context.WithCancel(c.Context())
		defer cancel()
		c.SetContext(ctx)
		return c.Next()
	})
	app.Get("/", func(c fiber.Ctx) error {
		calls.Add(1)
		return c.SendString("ok")
	})
	for range 2 {
		fiberCacheTestRequest(t, app, fiber.MethodGet, "http://example.test/", "", nil)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

// fiberCacheTestRequest 通过真实 Fiber 测试连接发送请求, 复制正文及响应头后关闭响应资源.
func fiberCacheTestRequest(t *testing.T, app *fiber.App, method, target, body string, headers http.Header) ([]byte, http.Header) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for name, values := range headers {
		req.Header[name] = append([]string(nil), values...)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return result, resp.Header.Clone()
}

// TestFiberCacheEndToEndHeaders 保留安全策略和业务元数据, 同时验证默认文本类型不会覆盖缓存 JSON.
// 外层本次请求的非表示头包括空值优先, 缓存正文对应的表示头仍必须由条目恢复.
func TestFiberCacheEndToEndHeaders(t *testing.T) {
	app := fiber.New()
	calls := 0
	expected := http.Header{
		"Content-Security-Policy": {"default-src 'none'; frame-ancestors 'none'"},
		"X-Content-Type-Options":  {"nosniff"},
		"Content-Location":        {"/canonical"},
		"Link":                    {"</items?page=2>; rel=next", "</items?page=1>; rel=prev"},
		"X-Total-Count":           {"42"},
	}
	app.Use(func(c fiber.Ctx) error {
		if override := c.Get("X-Outer-Policy"); override != "" {
			c.Set("Content-Security-Policy", override)
		}
		if c.Get("X-Outer-Empty") != "" {
			c.Set("Content-Security-Policy", "")
		}
		return c.Next()
	}, CacheByRequestURI(time.Minute))
	app.Get("/items", func(c fiber.Ctx) error {
		calls++
		for name, values := range expected {
			for _, value := range values {
				c.Response().Header.Add(name, value)
			}
		}
		return c.JSON(fiber.Map{"items": []int{1, 2}})
	})
	for range 2 {
		_, header := fiberCacheTestRequest(t, app, "GET", "http://example.test/items", "", nil)
		for name, values := range expected {
			if got := header.Values(name); strings.Join(got, "\n") != strings.Join(values, "\n") {
				t.Fatalf("header %s = %v, want %v", name, got, values)
			}
		}
		if !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
			t.Fatalf("content type = %q", header.Get("Content-Type"))
		}
	}
	const override = "default-src 'self'"
	_, header := fiberCacheTestRequest(t, app, "GET", "http://example.test/items", "", http.Header{"X-Outer-Policy": {override}})
	if header.Get("Content-Security-Policy") != override {
		t.Fatal("cached security policy overwrote current outer policy")
	}
	_, header = fiberCacheTestRequest(t, app, "GET", "http://example.test/items", "", http.Header{"X-Outer-Empty": {"yes"}})
	if value, exists := header["Content-Security-Policy"]; !exists || len(value) != 1 || value[0] != "" {
		t.Fatalf("explicit empty outer policy lost: %v", header)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

// TestFiberCacheReplayAbsentRepresentation 验证业务冷请求删除外层语言头后,
// 缓存回放也保持该头缺失, 不重新暴露外层默认值.
func TestFiberCacheReplayAbsentRepresentation(t *testing.T) {
	app := fiber.New()
	var calls atomic.Int32
	app.Use(func(c fiber.Ctx) error {
		c.Set("Content-Language", "en")
		return c.Next()
	})
	app.Get("/", CacheByRequestURI(time.Minute), func(c fiber.Ctx) error {
		calls.Add(1)
		c.Response().Header.Del("Content-Language")
		return c.SendString("ok")
	})
	for i := range 2 {
		body, header := fiberCacheTestRequest(t, app, http.MethodGet, "http://example.test/", "", nil)
		if string(body) != "ok" || len(header.Values("Content-Language")) != 0 {
			t.Fatalf("request %d: body = %q, language = %q", i, body, header.Values("Content-Language"))
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

// TestFiberCacheRequestTrailers 使用真实 HTTP 连接发送 chunked POST, 请求 trailer 不参与普通正文缓存.
func TestFiberCacheRequestTrailers(t *testing.T) {
	app := fiber.New()
	var calls atomic.Int32
	app.Use(CacheByRawData(time.Minute))
	app.Post("/", func(c fiber.Ctx) error {
		calls.Add(1)
		return c.SendString(c.Get("X-Tenant"))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true})
	}()
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(time.Second); err != nil {
			t.Errorf("shutdown trailer server: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("serve trailer request: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("trailer server did not stop")
		}
	})
	client := &http.Client{Timeout: 5 * time.Second}
	for _, tenant := range []string{"alice", "bob"} {
		req, err := http.NewRequest("POST", "http://"+listener.Addr().String(), strings.NewReader("query"))
		if err != nil {
			t.Fatal(err)
		}
		req.Trailer = http.Header{"X-Tenant": {tenant}}
		req.ContentLength = -1
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil || string(body) != tenant {
			t.Fatalf("tenant = %s, body = %q, read = %v, close = %v", tenant, body, readErr, closeErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("trailer request cached: handler calls = %d", calls.Load())
	}
}

// fiberCacheAwait 等待条件成立或超时. 超时不报错, 让串行化的实现也能结束并由峰值断言报告失败.
func fiberCacheAwait(cond func() bool, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for !cond() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// TestFiberCacheUncacheableConcurrency 验证 hit-for-pass: 领头请求的响应未准入时, 已排队的同键请求立即并发回源,
// 直通窗口内新到达的请求也不再排队; 可缓存响应仍只回源一次.
func TestFiberCacheUncacheableConcurrency(t *testing.T) {
	const requests = 5
	for _, tc := range []struct {
		name    string
		respond func(c fiber.Ctx) error
	}{
		{"cacheable", func(c fiber.Ctx) error { return c.SendString("data") }},
		{"no-store", func(c fiber.Ctx) error { c.Set("Cache-Control", "no-store"); return c.SendString("data") }},
		{"oversize", func(c fiber.Ctx) error { return c.SendString("123456789") }},
		{"error", func(c fiber.Ctx) error { return c.Status(fiber.StatusInternalServerError).SendString("data") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var arrived, calls, active, peak atomic.Int32
			// leader 和 want 只在两轮请求之间由测试协程修改, WaitGroup 保证可见性.
			leader, want := true, int32(requests-1)
			app.Use(func(c fiber.Ctx) error {
				arrived.Add(1)
				return c.Next()
			}, CacheByRequestURI(time.Minute, CacheConfig{MaxBodyBytes: 8}))
			app.Get("/", func(c fiber.Ctx) error {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				if calls.Add(1) == 1 && leader {
					// 领头请求等全部请求进入缓存层后再返回, 确保其余请求确实在键锁上排队.
					fiberCacheAwait(func() bool { return arrived.Load() == requests }, 5*time.Second)
					time.Sleep(20 * time.Millisecond)
				} else {
					fiberCacheAwait(func() bool { return peak.Load() >= want }, 500*time.Millisecond)
				}
				return tc.respond(c)
			})
			burst := func() {
				var group sync.WaitGroup
				for range requests {
					group.Go(func() {
						req := httptest.NewRequest(fiber.MethodGet, "http://example.test/", nil)
						resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
						if err != nil {
							t.Error(err)
							return
						}
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
					})
				}
				group.Wait()
			}
			burst()
			if tc.name == "cacheable" {
				if calls.Load() != 1 {
					t.Fatalf("cacheable concurrent miss executed %d times", calls.Load())
				}
				return
			}
			if calls.Load() != requests || peak.Load() < 2 {
				t.Fatalf("queued waiters: calls = %d, peak concurrency = %d, want %d calls and peak > 1",
					calls.Load(), peak.Load(), requests)
			}
			// 直通窗口内的新请求全部跳过键锁, 能够同时进入业务.
			leader, want = false, requests
			peak.Store(0)
			burst()
			if calls.Load() != 2*requests || peak.Load() != requests {
				t.Fatalf("pass window: calls = %d, peak concurrency = %d, want %d and %d",
					calls.Load(), peak.Load(), 2*requests, requests)
			}
		})
	}
}

// TestFiberCacheHijackedResponse 验证接管连接的请求不入缓存. HijackSetNoResponse 时 Fiber 响应对象从未发出,
// 若被缓存, 后续请求会得到源站实际没有发送的内容, 并且跳过接管连接后的真实输出.
func TestFiberCacheHijackedResponse(t *testing.T) {
	app := fiber.New()
	app.Use(CacheByRequestURI(time.Minute))
	var calls atomic.Int32
	app.Get("/", func(c fiber.Ctx) error {
		body := fmt.Sprintf("hijacked:%d", calls.Add(1))
		c.RequestCtx().HijackSetNoResponse(true)
		c.RequestCtx().Hijack(func(conn net.Conn) {
			_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		})
		return c.SendString("placeholder")
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true})
	}()
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(time.Second); err != nil {
			t.Errorf("shutdown hijack server: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("serve hijack request: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("hijack server did not stop")
		}
	})
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	for i := 1; i <= 2; i++ {
		resp, err := client.Get("http://" + listener.Addr().String() + "/")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if want := fmt.Sprintf("hijacked:%d", i); readErr != nil || closeErr != nil || string(body) != want {
			t.Fatalf("body = %q, want %q, read = %v, close = %v", body, want, readErr, closeErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("hijacked response cached: handler calls = %d", calls.Load())
	}
}
