package xcrypto

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

const onlineTestsEnv = "PKG_ONLINE_TESTS"

// TestGetCertificate 使用受信任的本地 TLS 服务验证显式端口和 https:// 地址.
func TestGetCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(server.Close)

	host, _, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split local TLS address: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: host,
	}

	addresses := []string{server.Listener.Addr().String(), server.URL}
	for _, addr := range addresses {
		t.Run(addr, func(t *testing.T) {
			cert, err := GetCertificate("tcp", addr, time.Second, tlsConfig.Clone())
			if err != nil {
				t.Fatalf("get local TLS certificate: %v", err)
			}
			assert.Equal(t, server.Certificate().Raw, cert.Raw)
		})
	}
}

// TestGetCertificateInvalidAddress 验证空地址继续返回稳定的参数错误.
func TestGetCertificateInvalidAddress(t *testing.T) {
	cert, err := GetCertificate("tcp", "  ", time.Second, nil)
	assert.Nil(t, cert)
	assert.Equal(t, ErrInvalidParam, err)
}

// TestNormalizeCertificateAddress 覆盖默认端口、显式端口、URL 前缀和 IPv6 边界.
func TestNormalizeCertificateAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "hostname default port", addr: "example.com", want: "example.com:443"},
		{name: "hostname explicit port", addr: "example.com:8443", want: "example.com:8443"},
		{name: "URL default port", addr: "https://example.com", want: "example.com:443"},
		{name: "URL explicit port", addr: "https://example.com:8443", want: "example.com:8443"},
		{name: "IPv4 default port", addr: "127.0.0.1", want: "127.0.0.1:443"},
		{name: "IPv6 default port", addr: "::1", want: "[::1]:443"},
		{name: "bracketed IPv6 default port", addr: "[::1]", want: "[::1]:443"},
		{name: "IPv6 explicit port", addr: "[::1]:8443", want: "[::1]:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCertificateAddress(tt.addr)
			if err != nil {
				t.Fatalf("normalize certificate address: %v", err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestOnlineGetCertificate 仅在显式开启在线测试时验证公网证书链.
func TestOnlineGetCertificate(t *testing.T) {
	if os.Getenv(onlineTestsEnv) != "1" {
		t.Skip("Set PKG_ONLINE_TESTS=1 to run public TLS smoke tests")
	}

	for _, addr := range []string{"www.microsoft.com", "https://www.microsoft.com"} {
		cert, err := GetCertificate("tcp", addr, 5*time.Second, nil)
		if err != nil {
			t.Fatalf("get public TLS certificate from %s: %v", addr, err)
		}
		assert.NotNil(t, cert)
	}
}
