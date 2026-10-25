package httpcache

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPrepareRejectsUnsafeResponses 验证公开缓存不会传播会话、认证、连接级或旧新鲜度状态.
func TestPrepareRejectsUnsafeResponses(t *testing.T) {
	c := New(time.Minute, Config{})
	for _, name := range []string{
		"Set-Cookie", "Set-Cookie2", "WWW-Authenticate", "Content-Range", "Connection", "Keep-Alive",
		"Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding",
		"Upgrade", "Date", "Age", "Expires", "Pragma", "Trailer:Digest",
	} {
		t.Run(name, func(t *testing.T) {
			// 故意构造小写且值为空的头, 覆盖 Header.Get 无法判断存在性的边界.
			header := http.Header{strings.ToLower(name): {""}}
			if _, ok := c.Prepare(header, []byte("body"), http.StatusOK); ok {
				t.Fatal("unsafe response was accepted")
			}
		})
	}
	for _, status := range []int{100, 201, 204, 206, 301, 304, 400, 500} {
		if _, ok := c.Prepare(nil, nil, status); ok {
			t.Fatalf("non-200 status accepted: %d", status)
		}
	}
	if _, ok := c.Prepare(http.Header{"Content-Type": {"Text/Event-Stream; charset=utf-8"}}, nil, 200); ok {
		t.Fatal("event stream accepted")
	}
}

// TestPrepareCacheControl 验证最保守的正数 TTL 上限, 以及无效或禁用缓存的指令.
func TestPrepareCacheControl(t *testing.T) {
	c := New(2*time.Minute, Config{})
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"", 2 * time.Minute},
		{"public", 2 * time.Minute},
		{"max-age=30", 30 * time.Second},
		{"MAX-AGE=30", 30 * time.Second},
		{"s-maxage=60, max-age=30", 30 * time.Second},
		{"max-age=300", 2 * time.Minute},
		{`max-age="30"`, 30 * time.Second},
		{"private", 0},
		{`private="X-Private"`, 0},
		{"no-cache", 0},
		{"no-store", 0},
		{"max-age=0", 0},
		{"s-maxage=0", 0},
		{"max-age=-1", 0},
		{"max-age=+1", 0},
		{"max-age=bad", 0},
		{"max-age", 0},
		{"max-age=1, max-age=1", 0},
		{"max-age=4294967296", 0},
		{`max-age="30`, 0},
		{`max-age=30"`, 0},
		{`max-age=""30""`, 0},
		{"public, no-store, max-age=30", 0},
	} {
		t.Run(test.value, func(t *testing.T) {
			header := http.Header{"Cache-Control": {test.value}}
			entry, ok := c.Prepare(header, nil, http.StatusOK)
			if test.want == 0 {
				if ok {
					t.Fatal("unsafe cache control accepted")
				}
				return
			}
			if !ok || entry.TTL != test.want || entry.Header.Get("Cache-Control") != test.value {
				t.Fatalf("got %+v, %v; want TTL %v", entry, ok, test.want)
			}
		})
	}
	if _, ok := c.Prepare(http.Header{"Cache-Control": {"public", "private"}}, nil, 200); ok {
		t.Fatal("second cache control line was ignored")
	}
}

// TestPrepareVary 验证只有已进入请求键的响应变化维度能够共享, 多行 Vary 全部检查.
func TestPrepareVary(t *testing.T) {
	c := New(time.Minute, Config{Vary: []string{"X-Tenant"}})
	for value, want := range map[string]bool{
		"": true, "Accept": true, "accept-language, Origin, X-Tenant": true,
		"Content-Type, Content-Encoding": true, "*": false, "X-Unknown": false,
		"Accept, X-Unknown": false, "Accept,": false,
	} {
		if _, got := c.Prepare(http.Header{"Vary": {value}}, nil, 200); got != want {
			t.Errorf("Vary %q: got %v, want %v", value, got, want)
		}
	}
	if _, ok := c.Prepare(http.Header{"Vary": {"Accept", "X-Unknown"}}, nil, 200); ok {
		t.Fatal("second Vary line was ignored")
	}
}

// TestPrepareBoundsAndResponseMetadata 验证正文和头预算分别有界, 保留端到端元数据,
// 同时排除已知逐请求头, 避免命中时丢失安全策略或分页信息.
func TestPrepareBoundsAndResponseMetadata(t *testing.T) {
	c := New(time.Minute, Config{MaxBodyBytes: 3})
	if _, ok := c.Prepare(nil, []byte("abcd"), 200); ok {
		t.Fatal("oversized response body accepted")
	}
	if _, ok := c.Prepare(http.Header{"X-Large": {strings.Repeat("x", maxHeaderBytes)}}, nil, 200); ok {
		t.Fatal("oversized response headers accepted")
	}
	header := http.Header{
		"Content-Type": {"application/json"}, "Content-Encoding": {"gzip"},
		"Content-Language": {"zh-CN"}, "Content-Disposition": {"inline"},
		"Etag": {`"version"`}, "Last-Modified": {"Wed, 21 Oct 2015 07:28:00 GMT"},
		"Cache-Control": {"public, max-age=30"}, "Vary": {"Origin"},
		"Access-Control-Allow-Origin": {"https://example.com"}, "X-Request-Id": {"private-id"},
		"X-Ratelimit-Remaining": {"100"}, "Content-Length": {"3"},
		"Content-Security-Policy": {"default-src 'self'"}, "X-Content-Type-Options": {"nosniff"},
		"Content-Location": {"/catalog"}, "Link": {"</next>; rel=next", "</prev>; rel=prev"},
		"X-Total-Count": {"10"}, "X-Unknown": {"metadata"},
		"RateLimit": {"limit=100, remaining=99"}, "Server-Timing": {"db;dur=10"},
		"Traceparent": {"request-trace"}, "Tracestate": {"vendor=request"},
	}
	entry, ok := c.Prepare(header, []byte("abc"), 200)
	if !ok {
		t.Fatal("safe response metadata should be cacheable")
	}
	for _, name := range []string{
		"Access-Control-Allow-Origin", "X-Request-Id", "X-Ratelimit-Remaining", "Content-Length",
		"RateLimit", "Server-Timing", "Traceparent", "Tracestate",
	} {
		if entry.Header.Get(name) != "" {
			t.Errorf("dynamic header stored: %s", name)
		}
	}
	for _, name := range []string{
		"Content-Security-Policy", "X-Content-Type-Options", "Content-Location", "Link", "X-Total-Count", "X-Unknown",
	} {
		if strings.Join(entry.Header.Values(name), "\n") != strings.Join(header.Values(name), "\n") {
			t.Errorf("response metadata was lost: %s", name)
		}
	}
	header.Set("Content-Type", "changed")
	if entry.Header.Get("Content-Type") != "application/json" {
		t.Fatal("prepared headers alias the framework response")
	}
}
