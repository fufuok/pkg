package myip

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/fufuok/pkg/assert"
)

const (
	testIPv4 = "192.0.2.44"
	testIPv6 = "2001:db8::44"
)

// TestGetAPI 验证 API 响应会被裁剪、规范化并拒绝无效地址、截断响应和请求错误.
func TestGetAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ipv4":
			_, _ = w.Write([]byte(" \n" + testIPv4 + "\t"))
		case "/ipv6":
			_, _ = w.Write([]byte("2001:0db8:0:0:0:0:0:44"))
		case "/short":
			w.Header().Set("Content-Length", "64")
			_, _ = w.Write([]byte(testIPv4))
		default:
			_, _ = w.Write([]byte("not-an-ip"))
		}
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name   string
		url    string
		wantIP string
		wantOK bool
	}{
		{name: "ipv4 whitespace", url: server.URL + "/ipv4", wantIP: testIPv4, wantOK: true},
		{name: "ipv6 canonical", url: server.URL + "/ipv6", wantIP: testIPv6, wantOK: true},
		{name: "invalid body", url: server.URL + "/invalid", wantIP: "", wantOK: false},
		{name: "short body", url: server.URL + "/short", wantIP: "", wantOK: false},
		{name: "invalid url", url: "://invalid", wantIP: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIP, gotOK := getAPI(tt.url)
			assert.Equal(t, tt.wantIP, gotIP)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

// TestExternalIPSelection 验证严格地址族过滤、默认 IPv4 和显式 IPv6 分派.
func TestExternalIPSelection(t *testing.T) {
	server := newIPFixtureServer(t, nil)
	setExternalIPAPIs(
		t,
		[]string{server.URL + "/ipv6", server.URL + "/invalid", server.URL + "/ipv4"},
		[]string{server.URL + "/ipv4", server.URL + "/invalid", server.URL + "/ipv6"},
	)

	assert.Equal(t, testIPv4, ExternalIPv4())
	assert.Equal(t, testIPv6, ExternalIPv6())
	assert.Equal(t, testIPv4, ExternalIP())
	assert.Equal(t, testIPv4, ExternalIP("unsupported"))
	assert.Equal(t, testIPv6, ExternalIP("ipv6"))
}

// TestExternalIPAny 验证非严格快速返回、IPv6 fallback 和正数重试次数.
func TestExternalIPAny(t *testing.T) {
	t.Run("accept first parsed address", func(t *testing.T) {
		var ipv6Requests atomic.Int32
		server := newIPFixtureServer(t, func(path string) {
			if path == "/ipv6" {
				ipv6Requests.Add(1)
			}
		})
		setExternalIPAPIs(t, []string{server.URL + "/ipv6"}, []string{server.URL + "/invalid"})

		assert.Equal(t, testIPv6, ExternalIPAny())
		assert.Equal(t, int32(1), ipv6Requests.Load())
	})

	t.Run("fallback to ipv6 list", func(t *testing.T) {
		server := newIPFixtureServer(t, nil)
		setExternalIPAPIs(t, []string{server.URL + "/invalid"}, []string{server.URL + "/ipv6"})

		assert.Equal(t, testIPv6, ExternalIPAny())
	})

	t.Run("retry count", func(t *testing.T) {
		var ipv4Requests atomic.Int32
		var ipv6Requests atomic.Int32
		server := newIPFixtureServer(t, func(path string) {
			switch path {
			case "/invalid-v4":
				ipv4Requests.Add(1)
			case "/invalid-v6":
				ipv6Requests.Add(1)
			}
		})
		setExternalIPAPIs(t, []string{server.URL + "/invalid-v4"}, []string{server.URL + "/invalid-v6"})

		assert.Equal(t, "", ExternalIPAny(2, 99))
		assert.Equal(t, int32(3), ipv4Requests.Load())
		assert.Equal(t, int32(3), ipv6Requests.Load())
	})
}

// TestInternalIP 验证本地 UDP 出口地址、默认网络和失败返回.
func TestInternalIP(t *testing.T) {
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen local UDP fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close local UDP fixture: %v", err)
		}
	})

	address := listener.LocalAddr().String()
	assert.Equal(t, "127.0.0.1", InternalIP(address, "udp4"))
	assert.Equal(t, "127.0.0.1", InternalIP(address, ""))
	assert.Equal(t, "", InternalIP(address, "invalid-network"))
	assert.Equal(t, "", InternalIP("invalid-address", "udp4"))
	assert.Equal(t, "", InternalIP("", "invalid-network"))
}

// TestInternalIPWrappers 验证系统出口查询为空或返回匹配地址族的有效 IP.
// UDP connect 只选择本地路由且不发送数据, 因此本测试不依赖公网服务响应.
func TestInternalIPWrappers(t *testing.T) {
	assertOptionalIP(t, "InternalIPv4", InternalIPv4(), "ipv4")
	assertOptionalIP(t, "InternalIPv6", InternalIPv6(), "ipv6")
	assertOptionalIP(t, "InternalIPAny", InternalIPAny(), "")
}

// TestLocalInterfaceAddresses 验证本机接口结果排除环回和链路本地地址, 并遵守地址族过滤.
func TestLocalInterfaceAddresses(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list system interfaces: %v", err)
	}
	names := make([]string, 0, len(interfaces))
	for _, item := range interfaces {
		names = append(names, item.Name)
	}
	if len(names) > 0 {
		assert.Equal(t, "", LocalIP(names...))
	}
	assertOptionalIP(t, "LocalIP", LocalIP(), "ipv4")

	for _, ip := range LocalIPv4s() {
		assertUsableInterfaceIP(t, "LocalIPv4s", net.ParseIP(ip), "ipv4")
	}

	tests := []struct {
		name   string
		filter string
		family string
	}{
		{name: "all"},
		{name: "ipv4", filter: "ipv4", family: "ipv4"},
		{name: "ipv6 case insensitive", filter: "IPv6", family: "ipv6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var addresses map[string][]net.IP
			var err error
			if tt.filter == "" {
				addresses, err = InterfaceAddrs()
			} else {
				addresses, err = InterfaceAddrs(tt.filter)
			}
			if err != nil {
				t.Fatalf("list filtered interface addresses: %v", err)
			}
			for name, ips := range addresses {
				assert.False(t, name == "", "interface name must not be empty")
				for _, ip := range ips {
					assertUsableInterfaceIP(t, name, ip, tt.family)
				}
			}
		})
	}
}

// newIPFixtureServer 建立只返回文档保留地址的本地 HTTP 服务.
// observe 在响应写入前同步执行, 用于验证调用次数而不暴露 handler 并发状态.
func newIPFixtureServer(t *testing.T, observe func(path string)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if observe != nil {
			observe(r.URL.Path)
		}
		switch r.URL.Path {
		case "/ipv4":
			_, _ = w.Write([]byte(testIPv4))
		case "/ipv6":
			_, _ = w.Write([]byte(testIPv6))
		default:
			_, _ = w.Write([]byte("not-an-ip"))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// setExternalIPAPIs 临时替换公网 API 列表并在测试结束时完整恢复.
// 调用方不得并行运行, 避免包级可变状态与其他测试相互影响.
func setExternalIPAPIs(t *testing.T, ipv4, ipv6 []string) {
	t.Helper()
	original := externalIPAPI
	externalIPAPI = map[string][]string{
		"ipv4": append([]string(nil), ipv4...),
		"ipv6": append([]string(nil), ipv6...),
	}
	t.Cleanup(func() {
		externalIPAPI = original
	})
}

// assertOptionalIP 校验系统相关查询允许为空, 非空时必须为有效且匹配的地址族.
func assertOptionalIP(t *testing.T, source, value, family string) {
	t.Helper()
	if value == "" {
		return
	}
	assertUsableIP(t, source, net.ParseIP(value), family)
}

// assertUsableInterfaceIP 校验接口地址有效且不是包契约明确排除的地址.
func assertUsableInterfaceIP(t *testing.T, source string, ip net.IP, family string) {
	t.Helper()
	assertUsableIP(t, source, ip, family)
	assert.False(t, ip.IsLoopback(), source+" returned loopback IP")
	assert.False(t, ip.IsLinkLocalUnicast(), source+" returned link-local IP")
}

// assertUsableIP 校验地址存在并匹配可选的 IPv4/IPv6 地址族.
func assertUsableIP(t *testing.T, source string, ip net.IP, family string) {
	t.Helper()
	assert.NotNil(t, ip, source+" returned invalid IP")
	switch family {
	case "ipv4":
		assert.NotNil(t, ip.To4(), source+" returned non-IPv4 address")
	case "ipv6":
		assert.Nil(t, ip.To4(), source+" returned non-IPv6 address")
	}
}
