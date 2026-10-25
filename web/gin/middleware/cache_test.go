package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
)

// cacheGinRequest 每次创建独立入站请求, 让缓存键测试覆盖真实 Host 和原始 RequestURI.
func cacheGinRequest(engine http.Handler, method, target, body string, header http.Header) *httptest.ResponseRecorder {
	var reader io.Reader
	if method == http.MethodPost || body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header = header.Clone()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestGinCachePartitions 覆盖完整 URI, Host, 头和 POST 原始正文的隔离及逐请求头保留.
func TestGinCachePartitions(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(strconv.FormatBool(raw), func(t *testing.T) {
			engine := gin.New()
			var calls, requests atomic.Int32
			engine.Use(func(c *gin.Context) {
				c.Header("X-Request-ID", strconv.Itoa(int(requests.Add(1))))
				c.Next()
			})
			method := http.MethodGet
			if raw {
				method = http.MethodPost
				engine.Use(CacheByRawData(time.Minute, CacheConfig{Vary: []string{"X-Tenant"}}))
			} else {
				engine.Use(CacheByRequestURI(time.Minute, CacheConfig{Vary: []string{"X-Tenant"}}))
			}
			engine.Any("/*path", func(c *gin.Context) {
				data, err := io.ReadAll(c.Request.Body)
				if err != nil {
					t.Error(err)
				}
				c.Header("Vary", "X-Tenant")
				c.String(200, "%d:%s", calls.Add(1), data)
			})
			type variant struct {
				url, body string
				header    http.Header
			}
			cases := []variant{
				{url: "http://one.example/a?tag=z&tag=a"},
				{url: "http://two.example/a?tag=z&tag=a"},
				{url: "http://one.example/b?tag=z&tag=a"},
				{url: "http://one.example/a?tag=a&tag=z"},
				{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"Accept-Language": {"zh"}}},
				{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"Accept-Encoding": {"gzip"}}},
				{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"Origin": {"https://other.example"}}},
				{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"X-Tenant": {"a"}}},
				{url: "https://one.example/a?tag=z&tag=a"},
			}
			if raw {
				cases = append(
					cases,
					variant{url: "http://one.example/a?tag=z&tag=a", body: "first"},
					variant{url: "http://one.example/a?tag=z&tag=a", body: "second"},
					variant{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"Content-Type": {"application/json"}}},
					variant{url: "http://one.example/a?tag=z&tag=a", header: http.Header{"Content-Encoding": {"gzip"}}},
				)
			}
			for i, tc := range cases {
				first := cacheGinRequest(engine, method, tc.url, tc.body, tc.header)
				second := cacheGinRequest(engine, method, tc.url, tc.body, tc.header)
				if first.Code != 200 || second.Body.String() != first.Body.String() || calls.Load() != int32(i+1) {
					t.Fatalf("variant %d: first=%s second=%s calls=%d", i, first.Body.String(), second.Body.String(), calls.Load())
				}
				if first.Header().Get("X-Request-ID") == second.Header().Get("X-Request-ID") {
					t.Fatal("request ID was replayed")
				}
				if second.Header().Get("Age") == "" {
					t.Fatal("hit lacks Age")
				}
			}
		})
	}
}

// TestGinCacheBypassRequests 验证已有公开命中也不能覆盖私有, 条件或非 GET 请求.
func TestGinCacheBypassRequests(t *testing.T) {
	for _, header := range []http.Header{
		{"Authorization": {"Bearer test"}},
		{"Cookie": {"uid=one"}},
		{"Range": {"bytes=0-2"}},
		{"If-None-Match": {"test"}},
		{"Cache-Control": {"no-cache"}},
		{"Pragma": {"no-cache"}},
		{"Accept": {"text/event-stream"}},
		{"Connection": {"upgrade"}},
	} {
		t.Run(fmt.Sprint(header), func(t *testing.T) {
			engine := gin.New()
			var calls atomic.Int32
			engine.Use(CacheByRequestURI(time.Minute))
			engine.Any("/a", func(c *gin.Context) { c.String(200, "%d", calls.Add(1)) })
			cacheGinRequest(engine, "GET", "http://example.com/a", "", nil)
			rec := cacheGinRequest(engine, "GET", "http://example.com/a", "", header)
			if rec.Body.String() != "2" {
				t.Fatalf("bypassed request got cached response %q", rec.Body.String())
			}
			cacheGinRequest(engine, "POST", "http://example.com/a", "", nil)
			if calls.Load() != 3 {
				t.Fatal("POST entered GET cache")
			}
		})
	}
}

// TestGinCacheBypassResponses 覆盖不能共享的响应, 超大正文以及显式刷新语义.
func TestGinCacheBypassResponses(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		status int
		flush  bool
	}{
		{name: "cookie", header: http.Header{"Set-Cookie": {"uid=one"}}},
		{name: "private", header: http.Header{"Cache-Control": {"private"}}},
		{name: "no-store-second-line", header: http.Header{"Cache-Control": {"public", "no-store"}}},
		{name: "zero-age", header: http.Header{"Cache-Control": {"max-age=0"}}},
		{name: "unknown-vary", header: http.Header{"Vary": {"X-Unknown"}}},
		{name: "vary-star", header: http.Header{"Vary": {"*"}}},
		{name: "sse", header: http.Header{"Content-Type": {"text/event-stream"}}},
		{name: "error", status: 500},
		{name: "partial", status: 206},
		{name: "flush", flush: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			calls := 0
			engine.Use(CacheByRequestURI(time.Minute))
			engine.GET("/a", func(c *gin.Context) {
				calls++
				for name, values := range tc.header {
					c.Writer.Header()[name] = values
				}
				if tc.status != 0 {
					c.Status(tc.status)
				}
				_, _ = c.Writer.WriteString("data")
				if tc.flush {
					c.Writer.Flush()
				}
			})
			for range 2 {
				cacheGinRequest(engine, "GET", "http://example.com/a", "", nil)
			}
			if calls != 2 {
				t.Fatalf("unsafe response cached: calls=%d", calls)
			}
		})
	}
}

// TestGinCacheBodyLimits 验证超限只关闭缓存, 不截断业务收到或返回的正文.
func TestGinCacheBodyLimits(t *testing.T) {
	for _, unknownLength := range []bool{false, true} {
		t.Run(strconv.FormatBool(unknownLength), func(t *testing.T) {
			engine := gin.New()
			calls := 0
			engine.Use(CacheByRawData(time.Minute, CacheConfig{MaxRequestBodyBytes: 3, MaxBodyBytes: 4}))
			engine.POST("/a", func(c *gin.Context) {
				calls++
				data, err := io.ReadAll(c.Request.Body)
				if err != nil {
					t.Fatal(err)
				}
				c.Data(200, "text/plain", data)
			})
			for range 2 {
				req := httptest.NewRequest("POST", "/a", strings.NewReader("abcdef"))
				if unknownLength {
					req.ContentLength = -1
				}
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Body.String() != "abcdef" {
					t.Fatalf("body changed: %q", rec.Body.String())
				}
			}
			if calls != 2 {
				t.Fatal("oversized request cached")
			}
		})
	}
	engine := gin.New()
	calls := 0
	engine.Use(CacheByRequestURI(time.Minute, CacheConfig{MaxBodyBytes: 3}))
	engine.GET("/a", func(c *gin.Context) { calls++; c.String(200, "abcdef") })
	for range 2 {
		if rec := cacheGinRequest(engine, "GET", "/a", "", nil); rec.Body.String() != "abcdef" {
			t.Fatal("oversized response truncated")
		}
	}
	if calls != 2 {
		t.Fatal("oversized response cached")
	}
}

// TestGinCacheTTL 在虚拟时间验证命中不续期, 避免依赖真实调度速度.
func TestGinCacheTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := gin.New()
		var calls atomic.Int32
		engine.Use(CacheByRequestURI(time.Second))
		engine.GET("/a", func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "data")
		})
		cacheGinRequest(engine, "GET", "/a", "", nil)
		cacheGinRequest(engine, "GET", "/a", "", nil)
		if calls.Load() != 1 {
			t.Fatal("repeated request did not hit")
		}
		time.Sleep(2 * time.Second)
		cacheGinRequest(engine, "GET", "/a", "", nil)
		if calls.Load() != 2 {
			t.Fatal("TTL did not expire")
		}
	})
}

// TestGinCacheGenerationAge 不让首字节之后的慢生成过程重新取得完整 max-age.
func TestGinCacheGenerationAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		engine := gin.New()
		calls := 0
		engine.Use(CacheByRequestURI(time.Minute))
		engine.GET("/a", func(c *gin.Context) {
			calls++
			c.Header("Cache-Control", "max-age=1")
			_, _ = c.Writer.WriteString("first")
			time.Sleep(2 * time.Second)
			_, _ = c.Writer.WriteString("last")
		})
		for range 2 {
			cacheGinRequest(engine, "GET", "/a", "", nil)
		}
		if calls != 2 {
			t.Fatal("response stale before completion was cached")
		}
	})
}

// TestGinCacheConcurrentMiss 用通道控制首个回源, 不把 Mutex 争用和虚拟计时混合.
func TestGinCacheConcurrentMiss(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	engine.Use(CacheByRequestURI(time.Minute))
	engine.GET("/a", func(c *gin.Context) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		c.String(200, "data")
	})
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() { cacheGinRequest(engine, "GET", "/a", "", nil) })
	}
	<-entered
	close(release)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent miss executed %d times", calls.Load())
	}
}

// ginCacheFailingBody 模拟只在返回部分字节的同次 Read 上报告错误的请求流.
type ginCacheFailingBody struct {
	err          error
	read, closed bool
}

// Read 的错误不会在下次调用重报, 用于约束缓存预读不吞掉一次性传输错误.
func (r *ginCacheFailingBody) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, "partial"), r.err
}

// Close 记录关闭责任是否仍由业务或服务器调用原始正文承担.
func (r *ginCacheFailingBody) Close() error { r.closed = true; return nil }

// TestGinCachePreservesBodyError 验证失败预读之后业务仍得到原始字节和原始错误身份.
func TestGinCachePreservesBodyError(t *testing.T) {
	engine := gin.New()
	errBody := errors.New("body transfer failed")
	body := &ginCacheFailingBody{err: errBody}
	engine.Use(CacheByRawData(time.Minute))
	engine.POST("/a", func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		if !errors.Is(err, errBody) || string(data) != "partial" || body.closed {
			t.Fatalf("body=%q err=%v closed=%v", data, err, body.closed)
		}
		_ = c.Request.Body.Close()
		c.Status(400)
	})
	req := httptest.NewRequest("POST", "/a", nil)
	req.Body, req.ContentLength = body, -1
	engine.ServeHTTP(httptest.NewRecorder(), req)
	if !body.closed {
		t.Fatal("original body was not closed")
	}
}

// TestGinCacheExtremeRequestLimit 防止预读上限加一溢出后把不同 POST 当作空正文.
func TestGinCacheExtremeRequestLimit(t *testing.T) {
	engine := gin.New()
	calls := 0
	engine.Use(CacheByRawData(time.Minute, CacheConfig{MaxRequestBodyBytes: math.MaxInt}))
	engine.POST("/a", func(c *gin.Context) {
		calls++
		data, _ := io.ReadAll(c.Request.Body)
		c.Data(200, "text/plain", data)
	})
	for _, body := range []string{"first", "second", "first"} {
		if rec := cacheGinRequest(engine, "POST", "/a", body, nil); rec.Body.String() != body {
			t.Fatalf("body=%q got=%q", body, rec.Body.String())
		}
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

// TestGinCacheHeaderValueBoundaries 区分单行逗号值和多个头行, 并防止非规范键名掩盖凭据.
func TestGinCacheHeaderValueBoundaries(t *testing.T) {
	engine := gin.New()
	calls := 0
	engine.Use(CacheByRequestURI(time.Minute, CacheConfig{Vary: []string{"X-Tenant"}}))
	engine.GET("/a", func(c *gin.Context) {
		calls++
		c.String(200, "%d:%s", calls, c.GetHeader("X-Tenant"))
	})
	first := cacheGinRequest(engine, "GET", "/a", "", http.Header{"X-Tenant": {"a,b"}})
	second := cacheGinRequest(engine, "GET", "/a", "", http.Header{"X-Tenant": {"a", "b"}})
	if first.Body.String() != "1:a,b" || second.Body.String() != "2:a" {
		t.Fatalf("first=%s second=%s", first.Body.String(), second.Body.String())
	}
	cacheGinRequest(engine, "GET", "/a", "", http.Header{"X-Tenant": {"a,b"}, "authorization": {"Bearer test"}})
	if calls != 3 {
		t.Fatal("noncanonical authorization key bypassed privacy check")
	}
}

// ginCacheGzipWriter 模拟常见的流式 gzip ResponseWriter, 验证缓存与压缩装配顺序.
type ginCacheGzipWriter struct {
	gin.ResponseWriter
	gzip *gzip.Writer
}

// Write 将业务明文字节交给 gzip, 底层缓存应只看到真正发送的编码字节.
func (w *ginCacheGzipWriter) Write(p []byte) (int, error) { return w.gzip.Write(p) }

// WriteString 防止 Gin 文本渲染绕过压缩 writer.
func (w *ginCacheGzipWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// ginCacheGzip 只在声明 gzip 时改写 writer, 保留空编码请求的直通路径.
func ginCacheGzip(c *gin.Context) {
	if c.GetHeader("Accept-Encoding") != "gzip" {
		c.Next()
		return
	}
	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	writer := gzip.NewWriter(c.Writer)
	original := c.Writer
	c.Writer = &ginCacheGzipWriter{ResponseWriter: original, gzip: writer}
	c.Next()
	_ = writer.Close()
	c.Writer = original
}

// TestGinCacheCompression 验证推荐顺序缓存编码字节, 外层已有编码 writer 时安全直通.
func TestGinCacheCompression(t *testing.T) {
	for _, outer := range []bool{false, true} {
		t.Run(strconv.FormatBool(outer), func(t *testing.T) {
			engine := gin.New()
			calls := 0
			if outer {
				engine.Use(ginCacheGzip, CacheByRequestURI(time.Minute))
			} else {
				engine.Use(CacheByRequestURI(time.Minute), ginCacheGzip)
			}
			engine.GET("/a", func(c *gin.Context) { calls++; c.String(200, "compressed payload") })
			for range 2 {
				rec := cacheGinRequest(engine, "GET", "/a", "", http.Header{"Accept-Encoding": {"gzip"}})
				reader, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || string(decoded) != "compressed payload" {
					t.Fatalf("invalid gzip payload %q: %v", decoded, err)
				}
			}
			want := 1
			if outer {
				want = 2
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d", calls, want)
			}
		})
	}
}

// BenchmarkGinResponseCacheHit 测量固定 1KiB 正文热命中, 不把内存请求结果外推为网络吞吐.
func BenchmarkGinResponseCacheHit(b *testing.B) {
	engine := gin.New()
	engine.Use(CacheByRequestURI(time.Minute))
	payload := strings.Repeat("x", 1024)
	engine.GET("/a", func(c *gin.Context) { c.String(200, "%s", payload) })
	cacheGinRequest(engine, "GET", "/a", "", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cacheGinRequest(engine, "GET", "/a", "", nil)
	}
}

// TestGinCacheEndToEndHeaders 验证安全策略和业务元数据随正文回放, 外层当前策略包括显式空值优先.
func TestGinCacheEndToEndHeaders(t *testing.T) {
	engine := gin.New()
	calls := 0
	policy := "default-src 'none'; frame-ancestors 'none'"
	expected := http.Header{
		"Content-Security-Policy": {policy},
		"X-Content-Type-Options":  {"nosniff"},
		"Content-Location":        {"/canonical"},
		"Link":                    {"</items?page=2>; rel=next", "</items?page=1>; rel=prev"},
		"X-Total-Count":           {"42"},
	}
	engine.Use(func(c *gin.Context) {
		if override := c.GetHeader("X-Outer-Policy"); override != "" {
			c.Header("Content-Security-Policy", override)
		}
		if c.GetHeader("X-Outer-Empty") != "" {
			c.Writer.Header()["Content-Security-Policy"] = nil
		}
		c.Next()
	}, CacheByRequestURI(time.Minute))
	engine.GET("/items", func(c *gin.Context) {
		calls++
		for name, values := range expected {
			c.Writer.Header()[name] = append([]string(nil), values...)
		}
		c.JSON(200, gin.H{"items": []int{1, 2}})
	})
	for range 2 {
		rec := cacheGinRequest(engine, "GET", "/items", "", nil)
		for name, values := range expected {
			if got := rec.Header().Values(name); strings.Join(got, "\n") != strings.Join(values, "\n") {
				t.Fatalf("header %s = %v, want %v", name, got, values)
			}
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
		}
	}
	const override = "default-src 'self'"
	rec := cacheGinRequest(engine, "GET", "/items", "", http.Header{"X-Outer-Policy": {override}})
	if rec.Header().Get("Content-Security-Policy") != override {
		t.Fatal("cached security policy overwrote current outer policy")
	}
	rec = cacheGinRequest(engine, "GET", "/items", "", http.Header{"X-Outer-Empty": {"yes"}})
	if value, exists := rec.Header()["Content-Security-Policy"]; !exists || len(value) != 0 {
		t.Fatalf("explicit empty outer policy lost: %v", rec.Header())
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

// TestGinCacheResponseTrailers 使用真实 HTTP 连接验证提前或写正文后声明的 trailer 均不被缓存.
// 大于 net/http 自动定长缓冲的正文触发普通 chunked 输出, 不借助 Flush 绕过缓存.
func TestGinCacheResponseTrailers(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(strconv.FormatBool(late), func(t *testing.T) {
			engine := gin.New()
			var calls atomic.Int32
			payload := strings.Repeat("x", 4096)
			engine.Use(CacheByRequestURI(time.Minute))
			engine.GET("/", func(c *gin.Context) {
				calls.Add(1)
				if !late {
					c.Writer.Header().Set(http.TrailerPrefix+"Digest", "sha256=result")
				}
				_, _ = c.Writer.WriteString(payload)
				if late {
					c.Writer.Header().Set(http.TrailerPrefix+"Digest", "sha256=result")
				}
			})
			server := httptest.NewServer(engine)
			defer server.Close()
			for range 2 {
				resp, err := http.Get(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil || closeErr != nil || string(body) != payload ||
					resp.Trailer.Get("Digest") != "sha256=result" {
					t.Fatalf("body bytes = %d, trailers = %v, read = %v, close = %v", len(body), resp.Trailer, readErr, closeErr)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("trailer response cached: handler calls = %d", calls.Load())
			}
		})
	}
}

// TestGinCacheRequestTrailers 保留读取完整正文后才可用的请求 trailer, 避免它们在正文键之外串用.
func TestGinCacheRequestTrailers(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(CacheByRawData(time.Minute))
	engine.POST("/", func(c *gin.Context) {
		calls.Add(1)
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			_ = c.Error(err)
			c.Status(400)
			return
		}
		c.String(200, "%s", c.Request.Trailer.Get("X-Tenant"))
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	for _, tenant := range []string{"alice", "bob"} {
		req, err := http.NewRequest("POST", server.URL, strings.NewReader("query"))
		if err != nil {
			t.Fatal(err)
		}
		req.Trailer = http.Header{"X-Tenant": {tenant}}
		req.ContentLength = -1
		resp, err := http.DefaultClient.Do(req)
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
