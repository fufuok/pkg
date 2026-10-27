package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
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

	"github.com/fufuok/pkg/web/internal/httpcache"
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
				maps.Copy(c.Writer.Header(), tc.header)
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

// slowCommitWriter 模拟首次提交本身阻塞的外层 writer (慢连接或阻塞的包装), 提交耗时 2 秒.
type slowCommitWriter struct {
	gin.ResponseWriter
	slept bool
}

// block 只让首次提交阻塞一次.
func (w *slowCommitWriter) block() {
	if !w.slept {
		w.slept = true
		time.Sleep(2 * time.Second)
	}
}

// WriteHeaderNow 提交后阻塞.
func (w *slowCommitWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
	w.block()
}

// Write 写出后阻塞.
func (w *slowCommitWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.block()
	return n, err
}

// TestGinCacheSlowFirstCommitAge 覆盖首次提交本身阻塞: 年龄从提交开始前计算, 头快照取提交之前.
// max-age=1 而首次提交耗时 2 秒, 保存时已经过期, 不得缓存.
func TestGinCacheSlowFirstCommitAge(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(c *gin.Context)
	}{
		{name: "write", write: func(c *gin.Context) { c.String(200, "body") }},
		{name: "header-now", write: func(c *gin.Context) { c.Status(200); c.Writer.WriteHeaderNow() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				engine := gin.New()
				engine.Use(func(c *gin.Context) {
					c.Writer = &slowCommitWriter{ResponseWriter: c.Writer}
					c.Next()
				}, CacheByRequestURI(time.Minute))
				calls := 0
				engine.GET("/", func(c *gin.Context) {
					calls++
					c.Header("Cache-Control", "max-age=1")
					tc.write(c)
				})
				for range 2 {
					cacheGinRequest(engine, http.MethodGet, "/", "", nil)
				}
				if calls != 2 {
					t.Fatalf("response expired during first commit was cached: calls = %d", calls)
				}
			})
		})
	}
}

// gzipOuterWriter 模拟 gzhttp 一类的 net/http 外层压缩: 提交时才设置 Content-Encoding 并压缩正文.
type gzipOuterWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	started bool
}

// start 首次提交时决定是否压缩, 已有编码的响应透传.
func (w *gzipOuterWriter) start() {
	if w.started {
		return
	}
	w.started = true
	if w.Header().Get("Content-Encoding") == "" {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.gz = gzip.NewWriter(w.ResponseWriter)
	}
}

// WriteHeader 提交前决定编码.
func (w *gzipOuterWriter) WriteHeader(code int) {
	w.start()
	w.ResponseWriter.WriteHeader(code)
}

// Write 按提交时的决定压缩或透传.
func (w *gzipOuterWriter) Write(data []byte) (int, error) {
	w.start()
	if w.gz != nil {
		return w.gz.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

// TestGinCacheOuterCommitTimeHeaders 覆盖外层 writer 在提交时才加入的字段: 头快照必须取自业务写出之前.
// net/http 外层压缩在提交时设置 Content-Encoding, 而缓存记录的是压缩前正文, 快照若含该头,
// 命中会把明文标成 gzip; 外层按访客补写的头同样不得被缓存并串给其他访客.
func TestGinCacheOuterCommitTimeHeaders(t *testing.T) {
	t.Run("gzip", func(t *testing.T) {
		engine := gin.New()
		var calls atomic.Int32
		engine.GET("/", CacheByRequestURI(time.Minute), func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "plain payload")
		})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gw := &gzipOuterWriter{ResponseWriter: w}
			engine.ServeHTTP(gw, r)
			if gw.gz != nil {
				_ = gw.gz.Close()
			}
		}))
		defer server.Close()
		for i := range 3 {
			resp, err := server.Client().Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr != nil || string(body) != "plain payload" {
				t.Fatalf("request %d: body = %q, err = %v", i, body, readErr)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", calls.Load())
		}
	})
	t.Run("visitor", func(t *testing.T) {
		engine := gin.New()
		engine.Use(func(c *gin.Context) {
			c.Writer = &visitorWriter{ResponseWriter: c.Writer, id: c.GetHeader("X-Test-Visitor")}
			c.Next()
		})
		var calls atomic.Int32
		engine.GET("/", CacheByRequestURI(time.Minute), func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "shared")
		})
		for _, id := range []string{"a", "b", "c"} {
			rec := cacheGinRequest(engine, http.MethodGet, "/", "", http.Header{"X-Test-Visitor": {id}})
			if got := rec.Header().Get("X-Visitor-Id"); got != id || rec.Body.String() != "shared" {
				t.Fatalf("visitor %s received id %q body %q", id, got, rec.Body.String())
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", calls.Load())
		}
	})
}

// visitorWriter 模拟外层 writer 在提交时按访客补写缺失的头.
type visitorWriter struct {
	gin.ResponseWriter
	id     string
	marked bool
}

// mark 首次提交时补写访客头, 已存在则保留.
func (w *visitorWriter) mark() {
	if !w.marked {
		w.marked = true
		if w.Header().Get("X-Visitor-Id") == "" {
			w.Header().Set("X-Visitor-Id", w.id)
		}
	}
}

// WriteHeaderNow 提交前补写.
func (w *visitorWriter) WriteHeaderNow() {
	w.mark()
	w.ResponseWriter.WriteHeaderNow()
}

// Write 首次写出前补写.
func (w *visitorWriter) Write(data []byte) (int, error) {
	w.mark()
	return w.ResponseWriter.Write(data)
}

// WriteString 首次写出前补写.
func (w *visitorWriter) WriteString(data string) (int, error) {
	w.mark()
	return w.ResponseWriter.WriteString(data)
}

// TestGinCacheEffectiveScheme 覆盖代理头中间件按 X-Forwarded-Scheme 等改写 r.URL.Scheme 的部署:
// 业务按改写后的协议生成内容时, 键必须随之分区, 否则单个伪造请求即可污染另一协议的共享条目.
func TestGinCacheEffectiveScheme(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Request.URL.Scheme = c.GetHeader("X-Test-Scheme")
		c.Next()
	})
	var calls atomic.Int32
	engine.GET("/", CacheByRequestURI(time.Minute), func(c *gin.Context) {
		calls.Add(1)
		c.String(200, "%s://example.test/next", c.Request.URL.Scheme)
	})
	for _, scheme := range []string{"https", "http"} {
		rec := cacheGinRequest(engine, http.MethodGet, "/", "", http.Header{"X-Test-Scheme": {scheme}})
		if want := scheme + "://example.test/next"; rec.Body.String() != want {
			t.Fatalf("scheme %s received %q", scheme, rec.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("effective scheme did not partition: handler calls = %d", calls.Load())
	}
}

// TestGinCacheRequestDoesNotAllocate 锁定请求快照零分配: 代理改写的 URL.Scheme 作为独立字段进入键,
// 不在每次命中拼接字符串; 其他字段均借用请求已有的字符串.
func TestGinCacheRequestDoesNotAllocate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/a?x=1", nil)
	req.URL.Scheme = "https"
	var sink httpcache.Request
	if allocs := testing.AllocsPerRun(100, func() { sink = ginCacheRequest(req) }); allocs != 0 {
		t.Fatalf("request snapshot allocates %.0f times per call", allocs)
	}
	if sink.EffectiveScheme != "https" {
		t.Fatalf("effective scheme = %q", sink.EffectiveScheme)
	}
}

// TestGinCacheReplayIsolation 验证命中回放的头值归本次响应独占: 外层在命中后原地改写或追加头值,
// 不得影响缓存条目和后续命中.
func TestGinCacheReplayIsolation(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Next()
		if values := c.Writer.Header()["X-Total-Count"]; len(values) > 0 {
			values[0] = "mutated"
			c.Writer.Header()["X-Total-Count"] = append(values, "extra")
		}
	})
	engine.GET("/", CacheByRequestURI(time.Minute), func(c *gin.Context) {
		c.Header("X-Total-Count", "42")
		c.String(200, "list")
	})
	for i := range 4 {
		// Result 取提交时的头快照, 外层之后的原地改写只作用于本次响应.
		sent := cacheGinRequest(engine, http.MethodGet, "/", "", nil).Result().Header
		if got := sent.Values("X-Total-Count"); len(got) != 1 || got[0] != "42" || i > 0 && sent.Get("Age") == "" {
			t.Fatalf("request %d sent X-Total-Count %q, Age %q", i, got, sent.Get("Age"))
		}
	}
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

// cacheFinalStatusWriter 模拟外层 writer 在实际写出前将状态改为失败.
type cacheFinalStatusWriter struct{ gin.ResponseWriter }

// Write 让状态变化发生在缓存首次快照之后, 验证最终状态仍决定能否保存.
func (w *cacheFinalStatusWriter) Write(data []byte) (int, error) {
	w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
	return w.ResponseWriter.Write(data)
}

// WriteString 保证文本写入经过与字节写入一致的状态变换.
func (w *cacheFinalStatusWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

// TestGinCacheFinalWriterStatus 验证复用首次头快照不会缓存随后被外层 writer 改成500的响应.
func TestGinCacheFinalWriterStatus(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Writer = &cacheFinalStatusWriter{ResponseWriter: c.Writer}
		c.Next()
	}, CacheByRequestURI(time.Minute))
	calls := 0
	engine.GET("/", func(c *gin.Context) {
		calls++
		c.String(http.StatusOK, "%d", calls)
	})
	for i := range 2 {
		rec := cacheGinRequest(engine, http.MethodGet, "/", "", nil)
		if rec.Code != http.StatusInternalServerError || rec.Body.String() != strconv.Itoa(i+1) {
			t.Fatalf("request %d: status=%d body=%q", i, rec.Code, rec.Body.String())
		}
	}
}

// TestGinCacheHeaderOnly 区分未提交与已提交的空响应, 不用正文长度推断是否可缓存.
func TestGinCacheHeaderOnly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		commit func(*gin.Context)
		calls  int
	}{
		{"automatic", func(*gin.Context) {}, 2},
		{"header-now", func(c *gin.Context) { c.Writer.WriteHeaderNow() }, 1},
		{"empty-write", func(c *gin.Context) { _, _ = c.Writer.Write(nil) }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(CacheByRequestURI(time.Minute))
			calls := 0
			engine.GET("/", func(c *gin.Context) {
				calls++
				c.Header("X-Result", "empty")
				c.Status(http.StatusOK)
				tc.commit(c)
			})
			for i := range 2 {
				rec := cacheGinRequest(engine, http.MethodGet, "/", "", nil)
				if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("X-Result") != "empty" {
					t.Fatalf("invalid header-only response: %+v", rec)
				}
				if got := rec.Header().Get("Age"); (got != "") != (i == 1 && tc.calls == 1) {
					t.Fatalf("request %d: unexpected Age %q", i, got)
				}
			}
			if calls != tc.calls {
				t.Fatalf("handler calls=%d, want %d", calls, tc.calls)
			}
		})
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

// TestGinCacheHeaderValueBoundaries 区分单行逗号值和多个头行, 并检查空首行后的凭据.
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
	cacheGinRequest(engine, "GET", "/a", "", http.Header{"X-Tenant": {"a,b"}, "Authorization": {"", "Bearer test"}})
	if calls != 3 {
		t.Fatal("second authorization value bypassed privacy check")
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
// 回放必须清除旧长度并交最终 writer 定界. 否则短长度令服务端截断并报 ErrContentLength,
// 长长度令客户端读到 unexpected EOF.
func TestGinCacheReplayContentLength(t *testing.T) {
	const payload = "cached response payload"
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "short", key: "Content-Length", value: "1"},
		{name: "long", key: "Content-Length", value: "999"},
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

// TestGinCacheReplayOuterBodyTransform 验证外层 writer 修改实际字节时, 命中不能
// 把缓存正文长度当成最终网络长度; 旧长度仍须清除, 让 net/http 计算真实长度.
func TestGinCacheReplayOuterBodyTransform(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(func(c *gin.Context) {
		c.Header("Content-Length", "100")
		original := c.Writer
		c.Writer = &cacheAppendWriter{ResponseWriter: original}
		c.Next()
		c.Writer = original
	}, CacheByRequestURI(time.Minute))
	engine.GET("/", func(c *gin.Context) {
		calls.Add(1)
		c.Writer.Header().Del("Content-Length")
		_, _ = c.Writer.WriteString("ok")
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	for i := range 2 {
		resp, err := server.Client().Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || string(body) != "ok-outer" || resp.ContentLength != int64(len(body)) {
			t.Fatalf("request %d: body = %q, length = %d, read = %v", i, body, resp.ContentLength, readErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

// cacheAppendWriter 模拟外层在写出时增加字节, 与压缩等正文变换具有相同长度边界.
type cacheAppendWriter struct{ gin.ResponseWriter }

// Write 返回输入长度以符合 writer 契约, 底层实际发送追加后的正文.
func (w *cacheAppendWriter) Write(body []byte) (int, error) {
	_, err := w.ResponseWriter.Write(append(bytes.Clone(body), "-outer"...))
	return len(body), err
}

// WriteString 与 Write 采用一致变换, 不让 Gin 的字符串写入绕过探针.
func (w *cacheAppendWriter) WriteString(body string) (int, error) {
	return w.Write([]byte(body))
}

// TestGinCacheReplayAbsentRepresentation 验证冷请求删除外层语言头后,
// 热命中仍须维持头不存在, 而非重新露出外层旧表示信息.
func TestGinCacheReplayAbsentRepresentation(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(func(c *gin.Context) {
		c.Header("Content-Language", "en")
		c.Next()
	}, CacheByRequestURI(time.Minute))
	engine.GET("/", func(c *gin.Context) {
		calls.Add(1)
		c.Writer.Header().Del("Content-Language")
		c.String(http.StatusOK, "ok")
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	for i := range 2 {
		resp, err := server.Client().Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || string(body) != "ok" || len(resp.Header.Values("Content-Language")) != 0 {
			t.Fatalf("request %d: body = %q, language = %q, read = %v", i, body, resp.Header.Values("Content-Language"), readErr)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}
}

// TestGinCacheOuterEncodingFieldsBypass 验证编码头首值为空时仍检查后续字段行.
func TestGinCacheOuterEncodingFieldsBypass(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(func(c *gin.Context) {
		c.Writer.Header()["Content-Encoding"] = []string{"", "identity"}
		c.Next()
	}, CacheByRequestURI(time.Minute))
	engine.GET("/", func(c *gin.Context) {
		calls.Add(1)
		_, _ = c.Writer.WriteString(strconv.Itoa(int(calls.Load())))
	})
	for i := range 2 {
		rec := cacheGinRequest(engine, http.MethodGet, "/", "", nil)
		if rec.Body.String() != strconv.Itoa(i+1) {
			t.Fatalf("request %d: body = %q, want own response", i, rec.Body.String())
		}
	}
}

// TestGinCacheNoncanonicalResponseHeaders 覆盖违反规范头名约定的处理器: 直接写入 Header map 的
// 小写 set-cookie 或 cache-control 会被 net/http 原样发送并由客户端遵从, 缓存必须拒绝共享该响应,
// 否则第二个用户会收到第一个用户的私有正文. 规范名的普通字段仍可缓存, 作为对照.
func TestGinCacheNoncanonicalResponseHeaders(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		calls      int32
	}{
		{key: "set-cookie", value: "uid=alice", calls: 2},
		{key: "cache-control", value: "private", calls: 2},
		{key: "cache-control", value: "no-store", calls: 2},
		{key: "x-trace", value: "1", calls: 2},
		{key: "X-Trace", value: "1", calls: 1},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			engine := gin.New()
			var calls atomic.Int32
			engine.GET("/me", CacheByRequestURI(time.Minute), func(c *gin.Context) {
				n := calls.Add(1)
				c.Writer.Header()[tc.key] = []string{tc.value}
				c.String(200, "user-%d", n)
			})
			for range 2 {
				cacheGinRequest(engine, http.MethodGet, "/me", "", nil)
			}
			if got := calls.Load(); got != tc.calls {
				t.Fatalf("handler calls = %d, want %d", got, tc.calls)
			}
		})
	}
}

// commitCookieWriter 模拟会话类外层 writer: 不在业务中设置 Cookie, 而在响应提交时才加入 Set-Cookie.
type commitCookieWriter struct {
	gin.ResponseWriter
	added bool
}

// addCookie 只在首次提交时加入一次会话 Cookie.
func (w *commitCookieWriter) addCookie() {
	if !w.added {
		w.added = true
		w.Header().Add("Set-Cookie", "sid=per-visitor")
	}
}

// WriteHeaderNow 在提交前加入 Cookie.
func (w *commitCookieWriter) WriteHeaderNow() {
	w.addCookie()
	w.ResponseWriter.WriteHeaderNow()
}

// Write 在首次写出正文前加入 Cookie.
func (w *commitCookieWriter) Write(data []byte) (int, error) {
	w.addCookie()
	return w.ResponseWriter.Write(data)
}

// WriteString 在首次写出正文前加入 Cookie.
func (w *commitCookieWriter) WriteString(data string) (int, error) {
	w.addCookie()
	return w.ResponseWriter.WriteString(data)
}

// TestGinCacheCommitTimeSetCookie 覆盖 Write 与显式 WriteHeaderNow 提交时加入的 Set-Cookie, 不覆盖 Gin 收尾自动提交.
// 快照取自底层提交前的头, 再用提交后的实时头复核, 避免依赖会话的正文被共享给其他新访客.
func TestGinCacheCommitTimeSetCookie(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(c *gin.Context, n int32)
	}{
		{name: "write", write: func(c *gin.Context, n int32) { c.String(200, "csrf-of-user-%d", n) }},
		{name: "header-now", write: func(c *gin.Context, _ int32) { c.Status(200); c.Writer.WriteHeaderNow() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Writer = &commitCookieWriter{ResponseWriter: c.Writer}
				c.Next()
			})
			var calls atomic.Int32
			engine.GET("/form", CacheByRequestURI(time.Minute), func(c *gin.Context) {
				tc.write(c, calls.Add(1))
			})
			for range 2 {
				if rec := cacheGinRequest(engine, http.MethodGet, "/form", "", nil); rec.Header().Get("Set-Cookie") == "" {
					t.Fatal("outer writer did not add Set-Cookie")
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("response with commit-time Set-Cookie was shared: handler calls = %d", calls.Load())
			}
		})
	}
}

// cacheCommitHeaderWriter 模拟 Gin 之外的 net/http 中间件, 在实际提交时才补充会话或隐私策略.
// 它包装底层 writer, 因而 Gin 收尾的自动提交也会经过这里.
type cacheCommitHeaderWriter struct {
	http.ResponseWriter
	name, value string
}

// WriteHeader 在底层提交之前加入本次请求的字段, 不依赖业务是否显式提交头.
func (w *cacheCommitHeaderWriter) WriteHeader(status int) {
	w.Header().Set(w.name, w.value)
	w.ResponseWriter.WriteHeader(status)
}

// TestGinCacheAutomaticCommitPolicy 通过真实 HTTP 验证自动提交才出现的 Cookie/private/no-store 不会漏过保护.
// 两次请求均不携带凭据或变化头, 响应 token 按业务执行生成, 避免把缺少 Vary 与提交时序混为一谈.
func TestGinCacheAutomaticCommitPolicy(t *testing.T) {
	for _, tc := range []struct{ name, header, value string }{
		{"cookie", "Set-Cookie", ""},
		{"private", "Cache-Control", "private"},
		{"no-store", "Cache-Control", "no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			var calls, requests atomic.Int32
			engine.GET("/", CacheByRequestURI(time.Minute), func(c *gin.Context) {
				c.Header("X-Csrf-Token", fmt.Sprintf("csrf-%d", calls.Add(1)))
				c.Status(http.StatusOK)
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				value := tc.value
				if n := requests.Add(1); tc.header == "Set-Cookie" {
					value = fmt.Sprintf("sid=visitor-%d", n)
				}
				engine.ServeHTTP(&cacheCommitHeaderWriter{ResponseWriter: w, name: tc.header, value: value}, r)
			}))
			defer server.Close()
			for i := 1; i <= 2; i++ {
				resp, err := server.Client().Get(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				value := tc.value
				if tc.header == "Set-Cookie" {
					value = fmt.Sprintf("sid=visitor-%d", i)
				}
				if resp.StatusCode != http.StatusOK || len(body) != 0 || resp.Header.Get(tc.header) != value ||
					resp.Header.Get("X-Csrf-Token") != fmt.Sprintf("csrf-%d", i) || resp.Header.Get("Age") != "" {
					t.Fatalf("request %d: status=%d headers=%v body=%q", i, resp.StatusCode, resp.Header, body)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("automatic commit response was cached: handler calls=%d", calls.Load())
			}
		})
	}
}

// TestGinCacheUnwrappedWrites 覆盖业务经 Unwrap 绕过记录写出: 首次经包装写出前底层已提交,
// 或全程未经包装, 记录的正文都不完整, 不得把空或截断正文保存为 200.
func TestGinCacheUnwrappedWrites(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(c *gin.Context)
	}{
		{name: "bypass", write: func(c *gin.Context) {
			raw := c.Writer.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
			_, _ = raw.Write([]byte("full body"))
		}},
		{name: "mixed", write: func(c *gin.Context) {
			raw := c.Writer.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
			_, _ = raw.Write([]byte("full "))
			_, _ = c.Writer.WriteString("body")
		}},
		{name: "wrapped-first", write: func(c *gin.Context) {
			_, _ = c.Writer.WriteString("full ")
			raw := c.Writer.(interface{ Unwrap() http.ResponseWriter }).Unwrap()
			_, _ = raw.Write([]byte("body"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			var calls atomic.Int32
			engine.GET("/raw", CacheByRequestURI(time.Minute), func(c *gin.Context) {
				calls.Add(1)
				tc.write(c)
			})
			for i := range 2 {
				if rec := cacheGinRequest(engine, http.MethodGet, "/raw", "", nil); rec.Body.String() != "full body" {
					t.Fatalf("request %d body = %q", i, rec.Body.String())
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("partially recorded response was cached: handler calls = %d", calls.Load())
			}
		})
	}
}

// ginCacheServeWithin 在限定时间内完成请求, 超时即判定缓存自锁; 失败后遗留的阻塞协程随测试进程退出.
func ginCacheServeWithin(t *testing.T, handler http.Handler, target string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- cacheGinRequest(handler, http.MethodGet, target, "", header) }()
	select {
	case rec := <-done:
		return rec
	case <-time.After(5 * time.Second):
		t.Fatalf("request %s deadlocked", target)
		return nil
	}
}

// TestGinCacheReentry 覆盖同一请求再次经过缓存: HandleContext 内部重路由会重置 c.Writer 并重新执行全局中间件,
// 同一实例也可能在路由组和路由上重复注册. 内层不得等待外层持有的键锁而自锁; 绕过记录 writer 写出的正文
// 不得被当作空响应缓存, 实际执行的路由仍可正常缓存和回放.
func TestGinCacheReentry(t *testing.T) {
	t.Run("global_handle_context", func(t *testing.T) {
		engine := gin.New()
		var calls atomic.Int32
		engine.Use(CacheByRequestURI(time.Minute))
		engine.GET("/a", func(c *gin.Context) {
			c.Request.URL.Path = "/b"
			engine.HandleContext(c)
		})
		engine.GET("/b", func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "hello-b")
		})
		for i := range 2 {
			if rec := ginCacheServeWithin(t, engine, "/a", nil); rec.Code != 200 || rec.Body.String() != "hello-b" {
				t.Fatalf("request %d: status = %d, body = %q", i, rec.Code, rec.Body.String())
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("rerouted response was not cached: handler calls = %d", calls.Load())
		}
	})
	t.Run("route_handle_context", func(t *testing.T) {
		engine := gin.New()
		var calls atomic.Int32
		engine.GET("/a", CacheByRequestURI(time.Minute), func(c *gin.Context) {
			c.Request.URL.Path = "/b"
			engine.HandleContext(c)
		})
		engine.GET("/b", func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "hello-b")
		})
		for i := range 2 {
			if rec := ginCacheServeWithin(t, engine, "/a", nil); rec.Code != 200 || rec.Body.String() != "hello-b" {
				t.Fatalf("request %d: status = %d, body = %q", i, rec.Code, rec.Body.String())
			}
		}
		if calls.Load() != 2 {
			t.Fatalf("response written around the recorder was cached: handler calls = %d", calls.Load())
		}
	})
	t.Run("double_registration", func(t *testing.T) {
		engine := gin.New()
		cache := CacheByRequestURI(time.Minute)
		var calls atomic.Int32
		engine.Group("/api", cache).GET("/x", cache, func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "x")
		})
		for i := range 2 {
			if rec := ginCacheServeWithin(t, engine, "/api/x", nil); rec.Code != 200 || rec.Body.String() != "x" {
				t.Fatalf("request %d: status = %d, body = %q", i, rec.Code, rec.Body.String())
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", calls.Load())
		}
	})
}

// TestGinCacheDeclaredLength 验证声明的 Content-Length 与记录的正文不符时不缓存: 业务写出中途失败并忽略错误时,
// 客户端收到的是截断响应, 命中由最终 writer 按缓存正文重新定界会把前缀伪装成完整结果. 一致的声明照常缓存.
func TestGinCacheDeclaredLength(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.GET("/:size", CacheByRequestURI(time.Minute), func(c *gin.Context) {
		calls.Add(1)
		c.Header("Content-Length", c.Param("size"))
		_, _ = c.Writer.WriteString("12345")
	})
	for _, tc := range []struct {
		size  string
		calls int32
	}{{"10", 2}, {"5", 1}} {
		calls.Store(0)
		for range 2 {
			cacheGinRequest(engine, http.MethodGet, "/"+tc.size, "", nil)
		}
		if calls.Load() != tc.calls {
			t.Fatalf("Content-Length %s: handler calls = %d, want %d", tc.size, calls.Load(), tc.calls)
		}
	}
}

// TestGinCacheOuterConditionalHeaders 验证外层在缓存之前按未进入键的输入设置的头不进入条目:
// 命中时外层会为当前请求重新设置, 存入条目只会把某个访客的值回放给其他访客. 业务设置的头照常回放.
func TestGinCacheOuterConditionalHeaders(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if strings.Contains(c.Request.UserAgent(), "internal") {
			c.Header("X-Debug-Backend", "10.0.0.5")
		}
		c.Header("X-Frame-Options", "DENY")
		c.Next()
	}, CacheByRequestURI(time.Minute))
	engine.GET("/p", func(c *gin.Context) {
		c.Header("X-Page", "1")
		c.String(200, "page")
	})
	first := cacheGinRequest(engine, http.MethodGet, "/p", "", http.Header{"User-Agent": {"internal-probe"}})
	if first.Header().Get("X-Debug-Backend") != "10.0.0.5" {
		t.Fatalf("outer header missing on miss: %v", first.Header())
	}
	for _, agent := range []string{"Mozilla/5.0", "internal-probe"} {
		hit := cacheGinRequest(engine, http.MethodGet, "/p", "", http.Header{"User-Agent": {agent}})
		want := map[string]string{"Mozilla/5.0": "", "internal-probe": "10.0.0.5"}[agent]
		if hit.Header().Get("Age") == "" || hit.Body.String() != "page" || hit.Header().Get("X-Debug-Backend") != want ||
			hit.Header().Get("X-Frame-Options") != "DENY" || hit.Header().Get("X-Page") != "1" {
			t.Fatalf("agent %s hit headers = %v, body = %q", agent, hit.Header(), hit.Body.String())
		}
	}
}

// bufferedOuterWriter 模拟处理结束后才提交的外层 net/http writer (如带最小压缩长度的 gzhttp):
// 业务首次写出后修改的头仍会随最终提交生效.
type bufferedOuterWriter struct {
	http.ResponseWriter
	header http.Header
	code   int
	body   bytes.Buffer
}

// Header 返回缓冲中的头集合, 提交前的修改都会生效.
func (w *bufferedOuterWriter) Header() http.Header { return w.header }

// WriteHeader 只记录首个状态码, 不立即提交.
func (w *bufferedOuterWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

// Write 只写入缓冲.
func (w *bufferedOuterWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(data)
}

// flush 以最终头集合提交缓冲的响应.
func (w *bufferedOuterWriter) flush() {
	maps.Copy(w.ResponseWriter.Header(), w.header)
	w.WriteHeader(http.StatusOK)
	w.ResponseWriter.WriteHeader(w.code)
	_, _ = w.ResponseWriter.Write(w.body.Bytes())
}

// TestGinCacheBufferedOuterLateHeaders 验证业务首次写出后改写表示头时不缓存: 缓冲型外层 writer 以最终头提交,
// 冷请求客户端看到的是新值, 首次写出时的快照已不能描述正文; 未改写的响应照常缓存.
func TestGinCacheBufferedOuterLateHeaders(t *testing.T) {
	engine := gin.New()
	var calls atomic.Int32
	engine.Use(CacheByRequestURI(time.Minute))
	engine.GET("/:mode", func(c *gin.Context) {
		calls.Add(1)
		c.Header("Content-Type", "text/plain")
		_, _ = c.Writer.WriteString(`{"a":1}`)
		if c.Param("mode") == "late" {
			c.Header("Content-Type", "application/json")
		}
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outer := &bufferedOuterWriter{ResponseWriter: w, header: make(http.Header)}
		engine.ServeHTTP(outer, r)
		outer.flush()
	})
	for _, tc := range []struct {
		path, contentType string
		calls             int32
	}{{"/late", "application/json", 2}, {"/fixed", "text/plain", 1}} {
		calls.Store(0)
		for i := range 2 {
			rec := cacheGinRequest(handler, http.MethodGet, tc.path, "", nil)
			if rec.Header().Get("Content-Type") != tc.contentType || rec.Body.String() != `{"a":1}` {
				t.Fatalf("%s request %d: type = %q, body = %q", tc.path, i, rec.Header().Get("Content-Type"), rec.Body.String())
			}
		}
		if calls.Load() != tc.calls {
			t.Fatalf("%s: handler calls = %d, want %d", tc.path, calls.Load(), tc.calls)
		}
	}
}

// TestGinCacheEffectiveRoute 验证外层按未进入键的请求头改写路径或查询时, 不同改写结果分别缓存:
// 键只含原始请求目标时, 移动端重写一类的入口会让桌面与移动响应互相覆盖.
func TestGinCacheEffectiveRoute(t *testing.T) {
	engine := gin.New()
	engine.Use(CacheByRequestURI(time.Minute))
	page := func(c *gin.Context) { c.String(200, c.Request.URL.Path+"?"+c.Request.URL.RawQuery) }
	engine.GET("/home", page)
	engine.GET("/m/home", page)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch agent := r.UserAgent(); {
		case strings.Contains(agent, "Mobile"):
			r.URL.Path = "/m" + r.URL.Path
		case strings.Contains(agent, "Lite"):
			r.URL.RawQuery = "lite=1"
		}
		engine.ServeHTTP(w, r)
	})
	for range 2 {
		for agent, want := range map[string]string{"Desktop": "/home?", "Mobile": "/m/home?", "Lite": "/home?lite=1"} {
			if rec := cacheGinRequest(handler, http.MethodGet, "/home", "", http.Header{"User-Agent": {agent}}); rec.Body.String() != want {
				t.Fatalf("agent %s body = %q, want %q", agent, rec.Body.String(), want)
			}
		}
	}
}

// TestGinCacheInnerDerivedContext 验证取消状态以进入缓存时的请求上下文为准: 内层超时中间件替换的派生上下文
// 在其返回时被 cancel, 不代表客户端断开. 正常完成的响应照常缓存; 不可缓存的领头响应仍登记直通窗口,
// 后续请求并发执行, 而不是被误判为放弃领头后逐个串行.
func TestGinCacheInnerDerivedContext(t *testing.T) {
	derived := func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Minute)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
	t.Run("cacheable", func(t *testing.T) {
		engine := gin.New()
		var calls atomic.Int32
		engine.GET("/", CacheByRequestURI(time.Minute), derived, func(c *gin.Context) {
			calls.Add(1)
			c.String(200, "ok")
		})
		for range 2 {
			cacheGinRequest(engine, http.MethodGet, "/", "", nil)
		}
		if calls.Load() != 1 {
			t.Fatalf("handler calls = %d, want 1", calls.Load())
		}
	})
	t.Run("uncacheable", func(t *testing.T) {
		const workers = 4
		engine := gin.New()
		var calls, active, peak atomic.Int32
		engine.GET("/", CacheByRequestURI(time.Minute), derived, func(c *gin.Context) {
			// 首个领头立即返回不可缓存响应; 其后的请求以峰值作屏障, 并发放行时同时到达, 串行时逐个等到超时.
			if calls.Add(1) > 1 {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				ginCacheAwait(func() bool { return peak.Load() >= workers-1 }, time.Second)
			}
			c.Header("Cache-Control", "no-store")
			c.String(200, "private")
		})
		cacheGinRequest(engine, http.MethodGet, "/", "", nil)
		var wg sync.WaitGroup
		for range workers - 1 {
			wg.Go(func() { cacheGinRequest(engine, http.MethodGet, "/", "", nil) })
		}
		wg.Wait()
		if peak.Load() != workers-1 {
			t.Fatalf("uncacheable requests were serialized: peak = %d", peak.Load())
		}
	})
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
		{"unsubmitted", func(c *gin.Context) { c.Header("X-Result", "empty"); c.Status(200) }},
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
