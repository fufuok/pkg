package httpcache

import (
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
		"query": func(r *Request) { r.URI = "/search?b=2&a=1" },
		"path":  func(r *Request) { r.URI = "/other?a=1&b=2" },
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
			req.Header = func(key string) string {
				if key == name {
					return "different"
				}
				return ""
			}
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
				req := Request{
					Header:       func(key string) string { return strings.Join(header.Values(key), ",") },
					HeaderValues: header.Values,
				}
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

// TestSkipRequest 验证敏感请求和协议控制始终回源, 多行控制头由适配器合并后检查.
func TestSkipRequest(t *testing.T) {
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Upgrade", "Range", "If-Match", "If-None-Match",
		"If-Modified-Since", "If-Unmodified-Since", "If-Range", "Cache-Control", "Pragma", "Trailer",
	} {
		t.Run(name, func(t *testing.T) {
			header := make(http.Header)
			header.Add(name, "value")
			req := Request{Header: func(key string) string { return strings.Join(header.Values(key), ",") }}
			if !SkipRequest(req) {
				t.Fatal("sensitive request was accepted")
			}
		})
	}
	for _, header := range []http.Header{
		{"Connection": {"keep-alive", "UpGrade"}},
		{"Accept": {"application/json", "TEXT/EVENT-STREAM; q=0.8"}},
		{"Cache-Control": {"", "no-cache"}},
	} {
		if !SkipRequest(Request{Header: func(key string) string { return strings.Join(header.Values(key), ",") }}) {
			t.Fatalf("protocol request was accepted: %v", header)
		}
	}
	if SkipRequest(Request{}) || SkipRequest(Request{Header: func(name string) string {
		if name == "Connection" {
			return "keep-alive, x-upgrade"
		}
		return ""
	}}) {
		t.Fatal("ordinary public request should be eligible")
	}
}
