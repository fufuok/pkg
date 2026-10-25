package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
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

// TestGinCacheReplayContentLength 覆盖外层预设长度头: 冷 miss 由业务修正长度, 热 hit 跳过业务,
// 回放必须按缓存正文重写长度并删除非规范大小写的同名键. 否则短长度令服务端截断并报 ErrContentLength,
// 长长度令客户端读到 unexpected EOF, 非规范键还会产生重复长度头.
func TestGinCacheReplayContentLength(t *testing.T) {
	const payload = "cached response payload"
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "short", key: "Content-Length", value: "1"},
		{name: "long", key: "Content-Length", value: "999"},
		{name: "noncanonical", key: "content-length", value: "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			var calls atomic.Int32
			engine.Use(func(c *gin.Context) {
				c.Writer.Header()[tc.key] = []string{tc.value}
				c.Next()
			})
			engine.Use(CacheByRequestURI(time.Minute))
			engine.GET("/", func(c *gin.Context) {
				calls.Add(1)
				for name := range c.Writer.Header() {
					if strings.EqualFold(name, "Content-Length") {
						delete(c.Writer.Header(), name)
					}
				}
				c.Header("Content-Length", strconv.Itoa(len(payload)))
				c.String(200, payload)
			})
			server := httptest.NewServer(engine)
			defer server.Close()
			for i := range 2 {
				resp, err := http.Get(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(resp.Body)
				closeErr := resp.Body.Close()
				if readErr != nil || closeErr != nil || string(body) != payload || len(resp.Header.Values("Content-Length")) != 1 {
					t.Fatalf("request %d: body = %q, read = %v, close = %v, lengths = %q",
						i, body, readErr, closeErr, resp.Header.Values("Content-Length"))
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("handler calls = %d, want 1", calls.Load())
			}
		})
	}
}

// TestGinCacheHTTP2Get 使用真实 TLS+HTTP/2 连接验证无正文 GET 可以命中.
// HTTP/2 服务端即使没有正文也提供非 nil 的 Body, 准入只能依据长度和传输编码判断;
// 声明长度或未知长度的 GET 正文仍须透传.
func TestGinCacheHTTP2Get(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(CacheByRequestURI(time.Minute))
	engine.GET("/a", func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		if err != nil {
			_ = c.Error(err)
			c.Status(400)
			return
		}
		c.String(200, "%d:%s", calls.Add(1), data)
	})
	server := httptest.NewUnstartedServer(engine)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	send := func(body io.Reader) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+"/a", body)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil || resp.StatusCode != 200 || resp.ProtoMajor != 2 {
			t.Fatalf("proto = %s, status = %d, read = %v, close = %v", resp.Proto, resp.StatusCode, readErr, closeErr)
		}
		return string(data)
	}
	for range 2 {
		if body := send(nil); body != "1:" {
			t.Fatalf("bodiless HTTP/2 GET body = %q, want cached 1:", body)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("bodiless HTTP/2 GET handler calls = %d, want 1", calls.Load())
	}
	// 已知长度和未知长度 (不发送 content-length, 流保持打开) 的 GET 正文都不能进入缓存.
	if body := send(strings.NewReader("x")); body != "2:x" {
		t.Fatalf("GET with body = %q, want 2:x", body)
	}
	if body := send(io.MultiReader(strings.NewReader("y"))); body != "3:y" {
		t.Fatalf("GET with unknown-length body = %q, want 3:y", body)
	}
}

// ginCacheConcurrency 统计业务处理的同时执行数, 用于区分串行回源和并发回源.
type ginCacheConcurrency struct {
	active, peak atomic.Int32
}

// enter 记录一次业务开始并更新峰值, 调用方负责在结束时递减 active.
func (p *ginCacheConcurrency) enter() {
	n := p.active.Add(1)
	for old := p.peak.Load(); n > old && !p.peak.CompareAndSwap(old, n); old = p.peak.Load() {
	}
}

// ginCacheAwait 等待条件成立或超时. 超时不报错, 让串行化的实现也能结束并由峰值断言报告失败.
func ginCacheAwait(cond func() bool, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for !cond() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// TestGinCacheUncacheableConcurrency 验证 hit-for-pass: 领头请求的响应未准入时, 已排队的同键请求立即并发回源,
// 直通窗口内新到达的请求也不再排队; 可缓存响应仍只回源一次.
func TestGinCacheUncacheableConcurrency(t *testing.T) {
	const requests = 5
	for _, tc := range []struct {
		name    string
		respond func(c *gin.Context)
	}{
		{"cacheable", func(c *gin.Context) { c.String(200, "data") }},
		{"no-store", func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.String(200, "data") }},
		{"oversize", func(c *gin.Context) { c.String(200, "123456789") }},
		{"error", func(c *gin.Context) { c.String(500, "data") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			var arrived, calls atomic.Int32
			probe := &ginCacheConcurrency{}
			// leader 和 want 只在两轮请求之间由测试协程修改, WaitGroup 保证可见性.
			leader, want := true, int32(requests-1)
			engine.Use(func(c *gin.Context) {
				arrived.Add(1)
				c.Next()
			}, CacheByRequestURI(time.Minute, CacheConfig{MaxBodyBytes: 8}))
			engine.GET("/a", func(c *gin.Context) {
				probe.enter()
				defer probe.active.Add(-1)
				if calls.Add(1) == 1 && leader {
					// 领头请求等全部请求进入缓存层后再返回, 确保其余请求确实在键锁上排队.
					ginCacheAwait(func() bool { return arrived.Load() == requests }, 5*time.Second)
					time.Sleep(20 * time.Millisecond)
				} else {
					ginCacheAwait(func() bool { return probe.peak.Load() >= want }, 500*time.Millisecond)
				}
				tc.respond(c)
			})
			burst := func() {
				var group sync.WaitGroup
				for range requests {
					group.Go(func() { cacheGinRequest(engine, "GET", "/a", "", nil) })
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
			if calls.Load() != requests || probe.peak.Load() < 2 {
				t.Fatalf("queued waiters: calls = %d, peak concurrency = %d, want %d calls and peak > 1",
					calls.Load(), probe.peak.Load(), requests)
			}
			// 直通窗口内的新请求全部跳过键锁, 能够同时进入业务.
			leader, want = false, requests
			probe.peak.Store(0)
			burst()
			if calls.Load() != 2*requests || probe.peak.Load() != requests {
				t.Fatalf("pass window: calls = %d, peak concurrency = %d, want %d and %d",
					calls.Load(), probe.peak.Load(), 2*requests, requests)
			}
		})
	}
}

// TestGinCacheUndeclaredRequestTrailers 用原始 chunked 请求发送未声明的 trailer.
// net/http 只在正文 EOF 时才把这类 trailer 合并进 Request.Trailer, 预读之后必须再次检查.
func TestGinCacheUndeclaredRequestTrailers(t *testing.T) {
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
		conn, err := net.Dial("tcp", server.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		_, err = fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\n"+
			"Connection: close\r\n\r\n5\r\nquery\r\n0\r\nX-Tenant: %s\r\n\r\n", tenant)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = conn.Close()
		if readErr != nil || resp.StatusCode != 200 || string(body) != tenant {
			t.Fatalf("tenant = %s, status = %d, body = %q, read = %v", tenant, resp.StatusCode, body, readErr)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("undeclared trailer request cached: handler calls = %d", calls.Load())
	}
}

// ginCacheCancelContext 在首次 Err 检查时通知测试, 用于在准入之后和键锁等待期间确定性地取消请求.
type ginCacheCancelContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

// Err 透传底层取消状态, 首次调用只额外发出一次通知. 先读取状态再通知,
// 避免测试协程在本次检查返回前取消, 使准入检查本身观察到取消.
func (c *ginCacheCancelContext) Err() error {
	err := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return err
}

// TestGinCacheCanceledWaiter 验证等待键锁期间被取消的请求返回明确的 503, 不写出误导性的空 200.
// 领头响应使用 no-store, 等待者不可能在取锁前命中缓存; 领头请求在取消之后才放行,
// 因此无论等待者是在锁上等待还是随后进入直通窗口, 锁后检查都必然看到取消.
func TestGinCacheCanceledWaiter(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	engine.Use(CacheByRequestURI(time.Minute))
	engine.GET("/a", func(c *gin.Context) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		c.Header("Cache-Control", "no-store")
		c.String(200, "data")
	})
	leaderDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { leaderDone <- cacheGinRequest(engine, "GET", "/a", "", nil) }()
	<-entered
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &ginCacheCancelContext{Context: base, checked: make(chan struct{})}
	req := httptest.NewRequest(http.MethodGet, "/a", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		engine.ServeHTTP(rec, req)
	}()
	// 准入检查已经以未取消状态通过, 此后直到 Acquire 返回都没有其他取消检查.
	<-ctx.checked
	cancel()
	close(release)
	<-waiterDone
	leader := <-leaderDone
	if leader.Code != 200 || leader.Body.String() != "data" {
		t.Fatalf("leader status = %d, body = %q", leader.Code, leader.Body.String())
	}
	if rec.Code != http.StatusServiceUnavailable || rec.Body.Len() != 0 || calls.Load() != 1 {
		t.Fatalf("canceled waiter status = %d, body = %q, handler calls = %d", rec.Code, rec.Body.String(), calls.Load())
	}
}
