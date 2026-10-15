package tproxy

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// TestProxyHandlerHTTPResponse 校验固定私网上游可以使用, 请求与 HTTP 错误响应保持透传.
// 已有代理 IP 头继续透传, Host 使用创建处理器时指定的值, scheme 大小写不影响转发.
func TestProxyHandlerHTTPResponse(t *testing.T) {
	for _, tt := range []struct {
		name      string
		status    int
		scheme    string
		bareAddr  bool
		proxyHost string
		wantHost  string
	}{
		{name: "HTTP_success", status: http.StatusOK, proxyHost: "virtual.example:8080", wantHost: "virtual.example:8080"},
		{name: "uppercase_scheme", status: http.StatusOK, scheme: "HTTP", wantHost: "client.example:80"},
		{name: "mixed_case_scheme", status: http.StatusOK, scheme: "hTtP", wantHost: "client.example:80"},
		{name: "upstream_failure", status: http.StatusServiceUnavailable, proxyHost: "virtual.example:8080", wantHost: "virtual.example:8080"},
		{name: "bare_address_original_host", status: http.StatusOK, bareAddr: true, wantHost: "client.example:80"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const requestBody = `{"query":{"match_all":{}}}`
			const responseBody = `{"source":"upstream"}`
			const requestURI = "/api/search?trace=a%2Fb"
			received := make(chan *http.Request, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != requestBody {
					t.Errorf("upstream body/error = %q/%v, want %q/nil", body, err, requestBody)
				}
				received <- r
				w.Header().Set("X-Upstream", "preserved")
				w.Header().Set("Connection", "X-Hop")
				w.Header().Set("X-Hop", "remove")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, responseBody)
			}))
			defer upstream.Close()
			app := fiber.New()
			address := upstream.URL
			if tt.bareAddr {
				address = strings.TrimPrefix(address, "http://")
			} else if tt.scheme != "" {
				address = strings.Replace(address, "http://", tt.scheme+"://", 1)
			}
			app.Use(ProxyHandler([]string{address}, time.Second, tt.proxyHost))
			req := httptest.NewRequest(http.MethodPost, requestURI, strings.NewReader(requestBody))
			req.Host = "client.example:80"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(HeaderXProxyClientIP, "192.0.2.10")
			req.Header.Set("Connection", "X-Hop")
			req.Header.Set("X-Hop", "remove")
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tt.status || string(body) != responseBody || res.Header.Get("X-Upstream") != "preserved" || res.Header.Get("X-Hop") != "" {
				t.Fatalf("response status/body/headers = %d/%s/%v", res.StatusCode, body, res.Header)
			}
			outgoing := <-received
			if outgoing.Method != http.MethodPost || outgoing.RequestURI != requestURI || outgoing.Host != tt.wantHost ||
				outgoing.Header.Get("Content-Type") != "application/json" || outgoing.Header.Get(HeaderXProxyClientIP) != "192.0.2.10" || outgoing.Header.Get("X-Hop") != "" {
				t.Fatalf("upstream request = %s %s Host:%s headers:%v", outgoing.Method, outgoing.RequestURI, outgoing.Host, outgoing.Header)
			}
		})
	}
}

// TestProxyHandlerUpstreamConfig 覆盖裸地址、协议默认端口及 scheme 大小写, 不访问外部网络.
// 空列表、URL 解析失败、非 HTTP(S) 和缺主机在构造时失败.
func TestProxyHandlerUpstreamConfig(t *testing.T) {
	for _, tt := range []struct {
		name      string
		servers   []string
		wantPanic bool
	}{
		{name: "HTTP_default_port", servers: []string{"http://upstream.example"}},
		{name: "HTTPS_default_port", servers: []string{"https://upstream.example"}},
		{name: "uppercase_HTTP", servers: []string{"HTTP://upstream.example"}},
		{name: "mixed_case_HTTP", servers: []string{"hTtP://upstream.example"}},
		{name: "uppercase_HTTPS", servers: []string{"HTTPS://upstream.example"}},
		{name: "mixed_case_HTTPS", servers: []string{"hTtPs://upstream.example"}},
		{name: "bare_address", servers: []string{"upstream.example:8080"}},
		{name: "empty_list", wantPanic: true},
		{name: "invalid_scheme", servers: []string{"ftp://upstream.example:21"}, wantPanic: true},
		{name: "empty_host", servers: []string{"http://"}, wantPanic: true},
		{name: "malformed_URL", servers: []string{"http://[::1"}, wantPanic: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if didPanic := recover() != nil; didPanic != tt.wantPanic {
					t.Errorf("construction panic = %t, want %t", didPanic, tt.wantPanic)
				}
			}()
			_ = ProxyHandler(tt.servers, time.Second, "")
		})
	}
}

// TestProxyHandlerHTTPSToHTTP 使用真实 TLS 入口, 验证发送协议不继承入站 TLS 状态.
// App.Test 不建立 TLS 连接, 因此复用 httptest 的证书及受信客户端完成真实本机请求.
func TestProxyHandlerHTTPSToHTTP(t *testing.T) {
	const wantBody = "plain upstream"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, wantBody)
	}))
	defer upstream.Close()
	app := fiber.New()
	app.Use(ProxyHandler([]string{upstream.URL}, time.Second, ""))
	seed := httptest.NewTLSServer(http.NotFoundHandler())
	defer seed.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- app.Listener(tls.NewListener(listener, seed.TLS.Clone()), fiber.ListenConfig{DisableStartupMessage: true})
	}()
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(time.Second); err != nil {
			t.Errorf("shutdown TLS entry: %v", err)
		}
		select {
		case err := <-serverErr:
			if err != nil {
				t.Errorf("serve TLS entry: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("TLS entry did not stop")
		}
	})
	client := seed.Client()
	client.Timeout = 5 * time.Second
	res, err := client.Post("https://"+listener.Addr().String()+"/api/search", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || string(body) != wantBody {
		t.Fatalf("status/body = %d/%s, want 200/%s", res.StatusCode, body, wantBody)
	}
}

// TestProxyHandlerHTTPToHTTPS 必须连接 HTTPS 上游并执行证书校验, 不增加跳过校验的测试入口.
// 各种 scheme 大小写都应到达证书校验, 区分旧实现的入站协议不匹配错误.
func TestProxyHandlerHTTPToHTTPS(t *testing.T) {
	upstream := httptest.NewTLSServer(http.NotFoundHandler())
	defer upstream.Close()
	for _, scheme := range []string{"https", "HTTPS", "hTtPs"} {
		t.Run(scheme, func(t *testing.T) {
			var proxyErr error
			app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
				proxyErr = err
				return c.Status(fiber.StatusBadGateway).SendString("proxy failure")
			}})
			address := strings.Replace(upstream.URL, "https://", scheme+"://", 1)
			app.Use(ProxyHandler([]string{address}, time.Second, ""))
			res, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/search", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusBadGateway || proxyErr == nil || !strings.Contains(proxyErr.Error(), "certificate") {
				t.Fatalf("status/error = %d/%v, want TLS certificate verification failure", res.StatusCode, proxyErr)
			}
		})
	}
}

// TestProxyHandlerIPv6Upstream 通过真实 IPv6 loopback 检验双栈拨号, 无 IPv6 的环境可跳过.
func TestProxyHandlerIPv6Upstream(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	const wantBody = "IPv6 upstream"
	upstream := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, wantBody)
	})}}
	upstream.Start()
	defer upstream.Close()
	app := fiber.New()
	app.Use(ProxyHandler([]string{upstream.URL}, time.Second, ""))
	res, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/search", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK || string(body) != wantBody {
		t.Fatalf("status/body = %d/%s, want 200/%s", res.StatusCode, body, wantBody)
	}
}

// TestProxyHandlerHTTPSIPv6DefaultPort 验证 IPv6 上游缺省 HTTPS 端口可进入正常拨号或 TLS 阶段.
// 只连接本机默认端口, 不要求该端口运行服务, 也不放宽证书校验.
func TestProxyHandlerHTTPSIPv6DefaultPort(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	var proxyErr error
	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		proxyErr = err
		return c.Status(fiber.StatusBadGateway).SendString("proxy failure")
	}})
	app.Use(ProxyHandler([]string{"https://[::1]"}, time.Second, ""))
	res, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/search", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if proxyErr != nil && strings.Contains(proxyErr.Error(), "missing port") {
		t.Fatalf("error = %v, want normal dial or TLS handling with default HTTPS port", proxyErr)
	}
}

// TestProxyHandlerFailureClearsResponse 保留传输错误身份, 清除未完成的上游正文与响应头.
// 应用仍负责错误状态和回包, 包内不能提前写入特定业务的 JSON 信封.
func TestProxyHandlerFailureClearsResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusedAddress := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 20\r\nX-Upstream: stale\r\n\r\nshort")
		_ = buffered.Flush()
	}))
	defer upstream.Close()
	for _, tt := range []struct {
		name    string
		address string
		wantEOF bool
	}{
		{name: "connection_refused", address: refusedAddress},
		{name: "truncated_body", address: upstream.URL, wantEOF: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var returnedErr error
			app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
				if err != returnedErr {
					t.Errorf("error handler received %v, want original error %v", err, returnedErr)
				}
				if len(c.Response().Body()) != 0 || len(c.Response().Header.Peek("X-Upstream")) != 0 || len(c.Response().Header.Peek("X-Before")) != 0 {
					t.Errorf("stale response before error handler: headers=%s body=%q", c.Response().Header.Header(), c.Response().Body())
				}
				return c.Status(fiber.StatusBadGateway).SendString("application failure")
			}})
			handler := ProxyHandler([]string{tt.address}, time.Second, "")
			app.Use(func(c fiber.Ctx) error {
				c.Set("X-Before", "stale")
				returnedErr = handler(c)
				return returnedErr
			})
			res, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/search", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusBadGateway || returnedErr == nil {
				t.Fatalf("status/error = %d/%v, want application-defined 502 and transport error", res.StatusCode, returnedErr)
			}
			if tt.wantEOF && !errors.Is(returnedErr, io.ErrUnexpectedEOF) {
				t.Fatalf("error = %v, want original unexpected EOF", returnedErr)
			}
			if !tt.wantEOF {
				if dialErr, ok := errors.AsType[*net.OpError](returnedErr); !ok || dialErr.Op != "dial" {
					t.Fatalf("error = %v, want original dial error", returnedErr)
				}
			}
		})
	}
}

// TestProxyHandlerMultipleUpstreams 保留既有负载均衡能力, 不把通用处理器收窄为单上游.
func TestProxyHandlerMultipleUpstreams(t *testing.T) {
	servers := make([]string, 0, 2)
	for _, label := range []string{"first", "second"} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, label)
		}))
		t.Cleanup(upstream.Close)
		servers = append(servers, upstream.URL)
	}
	app := fiber.New()
	app.Use(ProxyHandler(servers, time.Second, ""))
	seen := make(map[string]bool)
	for range 4 {
		res, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/search", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("upstream status/error = %d/%v", res.StatusCode, err)
		}
		seen[string(body)] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("upstreams seen = %v, want both configured servers", seen)
	}
}

// TestProxyHandlerDefaultPathPolicy 锁定新版默认路径检查, 不恢复旧版的歧义路径放行.
// 被拒路径不能访问上游; 允许路径使用路由规范化结果, 查询串保留原编码.
func TestProxyHandlerDefaultPathPolicy(t *testing.T) {
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.RequestURI
		_, _ = io.WriteString(w, r.RequestURI)
	}))
	defer upstream.Close()
	app := fiber.New()
	app.Use(ProxyHandler([]string{upstream.URL}, time.Second, ""))
	for _, tt := range []struct {
		target string
		want   string
	}{
		{"/plain/path", "/plain/path"},
		{"/a%20b", "/a%20b"},
		{"/plain?x=a%2Fb&key=a%3Bb", "/plain?x=a%2Fb&key=a%3Bb"},
		{"/a/../b", "/b"},
		{"/a%2Fb", ""},
		{"/a//b", ""},
		{"/a%5Cb", ""},
		{"/a%3Bb", ""},
		{"/a%40b", ""},
		{"/a%3Ab", ""},
	} {
		t.Run(tt.target, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, tt.target, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if tt.want == "" {
				if resp.StatusCode != fiber.StatusBadRequest {
					t.Fatalf("status = %d, want 400", resp.StatusCode)
				}
				select {
				case uri := <-received:
					t.Fatalf("rejected path reached upstream: %q", uri)
				default:
				}
				return
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != tt.want || resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status/body/error = %d/%q/%v, want 200/%q/nil", resp.StatusCode, body, err, tt.want)
			}
			select {
			case uri := <-received:
				if uri != tt.want {
					t.Fatalf("upstream URI = %q, want %q", uri, tt.want)
				}
			default:
				t.Fatal("successful proxy request did not reach upstream")
			}
		})
	}
}
