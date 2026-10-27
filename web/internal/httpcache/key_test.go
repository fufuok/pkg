package httpcache

import (
	"crypto/sha256"
	"encoding/binary"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestKeyPartitionsRequest 验证全部协议和表示维度独立, 查询和正文不进行业务语义归一化.
func TestKeyPartitionsRequest(t *testing.T) {
	c := New(time.Minute, Config{Vary: []string{"X-Tenant"}})
	base := Request{Method: "POST", Scheme: "https", Host: "example.com", URI: "/search?a=1&b=2"}
	body := []byte(`{"q":"sample"}`)
	original := c.Key(base, body)
	for name, change := range map[string]func(*Request){
		"method": func(r *Request) { r.Method = "GET" },
		"scheme": func(r *Request) { r.Scheme = "http" },
		"host":   func(r *Request) { r.Host = "other.example.com" },
		"effective_host": func(r *Request) {
			r.EffectiveHost = "proxy.example.com"
		},
		"effective_scheme": func(r *Request) { r.EffectiveScheme = "http" },
		"query":            func(r *Request) { r.URI = "/search?b=2&a=1" },
		"path":             func(r *Request) { r.URI = "/other?a=1&b=2" },
		"effective_path":   func(r *Request) { r.Path = "/m/search" },
		"effective_query":  func(r *Request) { r.Query = "lite=1" },
	} {
		t.Run(name, func(t *testing.T) {
			req := base
			change(&req)
			if c.Key(req, body) == original {
				t.Fatal("request dimension missing from key")
			}
		})
	}
	for _, name := range []string{
		"Accept", "Accept-Encoding", "Accept-Language", "Origin", "Content-Type", "Content-Encoding", "X-Tenant",
	} {
		t.Run(name, func(t *testing.T) {
			req := base
			req.Header = http.Header{name: {"different"}}
			if c.Key(req, body) == original {
				t.Fatal("request representation missing from key")
			}
		})
	}
	if c.Key(base, []byte(`{"q":"other"}`)) == original || c.Key(base, nil) == original {
		t.Fatal("request body missing from key")
	}
	if c.Key(base, append([]byte(nil), body...)) != original {
		t.Fatal("equal body bytes should produce the same key")
	}
}

// TestKeyHeaderCardinality 验证固定头与自定义变化头逐行编码, 不将逗号分隔值或空行混为相同表示.
func TestKeyHeaderCardinality(t *testing.T) {
	c := New(time.Minute, Config{Vary: []string{"X-Tenant"}})
	for _, name := range []string{"Content-Type", "Content-Encoding", "X-Tenant", "Accept"} {
		t.Run(name, func(t *testing.T) {
			keys := make(map[[32]byte]bool)
			for _, values := range [][]string{nil, {""}, {"a,b"}, {"a", "b"}, {"b", "a"}, {"", "a,b"}} {
				header := http.Header{name: values}
				req := Request{Header: header}
				key := c.Key(req, nil)
				if keys[key] {
					t.Fatalf("header %s values %q collided with a different sequence", name, values)
				}
				keys[key] = true
			}
		})
	}
}

// TestKeyLengthDelimiters 防止简单拼接造成跨字段边界碰撞, 不依赖摘要算法找碰撞.
func TestKeyLengthDelimiters(t *testing.T) {
	c := New(time.Minute, Config{})
	left := Request{Method: "a", Scheme: "bc", Host: "d", URI: "e"}
	right := Request{Method: "ab", Scheme: "c", Host: "d", URI: "e"}
	if c.Key(left, nil) == c.Key(right, nil) {
		t.Fatal("field lengths were not delimited")
	}
}

// TestKeyBufferBoundaries 以独立拼接的参考编码核对键: 字段跨越栈缓冲区边界或超过整个缓冲区时,
// 成批写入与直接写入的摘要必须与一次性哈希完整材料相同.
func TestKeyBufferBoundaries(t *testing.T) {
	c := New(time.Minute, Config{Vary: []string{"X-Tenant"}})
	reference := func(req Request, body []byte) [32]byte {
		var material []byte
		value := func(s string) {
			material = binary.AppendUvarint(material, uint64(len(s)))
			material = append(material, s...)
		}
		for _, v := range []string{
			req.Method, req.Scheme, req.EffectiveScheme, req.Host, req.EffectiveHost, req.URI, req.Path, req.Query,
		} {
			value(v)
		}
		for _, name := range append(keyHeaders[:], c.cfg.Vary...) {
			material = binary.AppendUvarint(material, uint64(len(req.Header[name])))
			for _, v := range req.Header[name] {
				value(v)
			}
		}
		material = binary.AppendUvarint(material, uint64(len(body)))
		return sha256.Sum256(append(material, body...))
	}
	for _, size := range []int{0, 1, 127, 128, 490, 500, 505, 511, 512, 513, 1500} {
		long := strings.Repeat("v", size)
		req := Request{
			Method: "GET", Scheme: "https", EffectiveScheme: long, Host: "example.com", URI: "/" + long, Path: long, Query: long,
			Header: http.Header{"Accept": {long, "a"}, "X-Tenant": {"t", long}, "Content-Type": {long}},
		}
		body := []byte(long)
		if c.Key(req, body) != reference(req, body) {
			t.Fatalf("key for field size %d differs from reference encoding", size)
		}
	}
}

// TestSkipRequest 验证敏感请求和协议控制始终回源, 多行控制头由适配器合并后检查.
func TestSkipRequest(t *testing.T) {
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Upgrade", "Range", "If-Match", "If-None-Match",
		"If-Modified-Since", "If-Unmodified-Since", "If-Range", "Cache-Control", "Pragma", "Trailer",
	} {
		t.Run(name, func(t *testing.T) {
			header := make(http.Header)
			header.Add(name, "value")
			req := Request{Header: header}
			if !SkipRequest(req) {
				t.Fatal("sensitive request was accepted")
			}
		})
	}
	for _, header := range []http.Header{
		{"Connection": {"keep-alive", "UpGrade"}},
		{"Accept": {"application/json", "TEXT/EVENT-STREAM; q=0.8"}},
		{"Accept": {"text/html, text/event-stream;q=0.9"}},
		{"Cache-Control": {"", "no-cache"}},
	} {
		if !SkipRequest(Request{Header: header}) {
			t.Fatalf("protocol request was accepted: %v", header)
		}
	}
	// 事件流按媒体类型逐项匹配, 含相同子串的其他类型或参数不是流式请求.
	for _, header := range []http.Header{
		{},
		{"Connection": {"keep-alive, x-upgrade"}},
		{"Accept": {"application/x-www-form-urlencoded, text/event-streams"}},
		{"Accept": {`text/html;profile="text/event-stream"`}},
	} {
		if SkipRequest(Request{Header: header}) {
			t.Fatalf("ordinary public request should be eligible: %v", header)
		}
	}
}

// realisticRequest 是接近真实浏览器的请求: 多个值超过 32 字节, 并带 Connection: keep-alive.
func realisticRequest() Request {
	return Request{
		Method: "GET", Scheme: "https", Host: "shop.example.com",
		URI: "/api/v1/products?category=books&sort=price_desc&page=3&size=20", Header: http.Header{
			"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"},
			"Accept-Encoding": {"gzip, deflate, br, zstd"},
			"Accept-Language": {"zh-CN,zh;q=0.9,en;q=0.8,en-US;q=0.7"},
			"Connection":      {"keep-alive"},
			"User-Agent":      {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0 Safari/537.36"},
		},
	}
}

// TestHitPathDoesNotAllocate 锁定命中路径上的内核检查零分配: 长头值的键计算, 带 Connection 的请求跳过检查,
// 以及外层已设置 Vary 与 Cache-Control 时的回放前检查. 它们在每次命中都执行, 分配会随流量放大.
func TestHitPathDoesNotAllocate(t *testing.T) {
	c := New(time.Minute, Config{})
	req := realisticRequest()
	outer := http.Header{"Vary": {"Origin, Accept-Encoding"}, "Cache-Control": {"public, max-age=60"}}
	// 指令名大小写无关, 大写写法同样不得为转换小写分配.
	upper := http.Header{"Cache-Control": {"Public, MAX-AGE=60, S-MaxAge=30"}}
	for name, fn := range map[string]func(){
		"key":           func() { _ = c.Key(req, nil) },
		"skip":          func() { _ = SkipRequest(req) },
		"canServe":      func() { _ = c.CanServe(outer, http.StatusOK) },
		"canServeUpper": func() { _ = c.CanServe(upper, http.StatusOK) },
	} {
		if allocs := testing.AllocsPerRun(100, fn); allocs != 0 {
			t.Errorf("%s allocates %.0f times per call", name, allocs)
		}
	}
}

// TestKeyIncludesForwardedHeaders 验证转发头默认进入键. 应用按 X-Forwarded-Host/Proto 或 Forwarded
// 生成绝对地址时, 这些头若不分区, 单个伪造请求即可把恶意地址写入共享缓存 (缓存投毒).
func TestKeyIncludesForwardedHeaders(t *testing.T) {
	c := New(time.Minute, Config{})
	base := c.Key(realisticRequest(), nil)
	for name, value := range map[string]string{
		"X-Forwarded-Host": "evil.example", "X-Forwarded-Proto": "http", "Forwarded": "host=evil.example",
	} {
		req := realisticRequest()
		req.Header = req.Header.Clone()
		req.Header.Set(name, value)
		if c.Key(req, nil) == base {
			t.Errorf("%s does not partition the cache", name)
		}
	}
}

// TestRequestHeaderName 验证适配器按需收集请求头: 只接受策略与键需要的字段, 大小写无关并返回规范名,
// 使关闭头名规范化的框架仍能识别小写凭据, 无关字段不进入快照.
func TestRequestHeaderName(t *testing.T) {
	c := New(time.Minute, Config{Vary: []string{"X-Tenant"}})
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"Authorization", "Authorization", true},
		{"authorization", "Authorization", true},
		{"COOKIE", "Cookie", true},
		{"x-tenant", "X-Tenant", true},
		{"Content-Type", "Content-Type", true},
		{"X-Forwarded-Host", "X-Forwarded-Host", true},
		{"User-Agent", "", false},
		{"X-Tenant-Extra-Long-Header-Name-That-Exceeds-Any-Relevant-Name", "", false},
		{"", "", false},
	} {
		got, ok := c.RequestHeaderName([]byte(tc.name))
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q = %q, %v; want %q, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if allocs := testing.AllocsPerRun(100, func() { _, _ = c.RequestHeaderName([]byte("accept-encoding")) }); allocs != 0 {
		t.Errorf("RequestHeaderName allocates %.0f times per call", allocs)
	}
}
