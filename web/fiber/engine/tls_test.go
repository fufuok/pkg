package engine

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"

	"github.com/fufuok/pkg/config"
)

// TestRunOneSharedTLS 验证真实多 HTTPS 启动共享握手状态, 并在连接关闭后释放记录.
// 同时保持两连接握手后再发请求, 防止错误地退回只保存最后一次握手的实现.
func TestRunOneSharedTLS(t *testing.T) {
	certFile, keyFile, roots := writeTestTLSCertificate(t)
	ready := make(chan *fiber.App, 1)
	addresses := make(chan string, 2)
	closed := make(chan error, 2)
	banners := make(chan bool, 2)
	stopped := make(chan struct{})
	var setups atomic.Int32
	go func() {
		defer close(stopped)
		runOne(func(app *fiber.App) *fiber.App {
			setups.Add(1)
			app.Hooks().OnListen(func(data fiber.ListenData) error {
				addresses <- net.JoinHostPort(data.Host, data.Port)
				return nil
			})
			app.Hooks().OnPostStartupMessage(func(data *fiber.PostStartupMessageData) error {
				banners <- data.Disabled
				return nil
			})
			// Fiber 在调用用户 ConnState 前清理 TLS 记录, 用公开 API 验证关闭状态.
			app.Server().ConnState = func(conn net.Conn, state fasthttp.ConnState) {
				if state != fasthttp.StateClosed {
					return
				}
				var ctx fasthttp.RequestCtx
				ctx.Init2(conn, nil, false)
				c := app.AcquireCtx(&ctx)
				var err error
				if c.ClientHelloInfo() != nil {
					err = fmt.Errorf("closed connection retained ClientHelloInfo")
				}
				app.ReleaseCtx(c)
				closed <- err
			}
			app.Get("/", func(c fiber.Ctx) error {
				if info := c.ClientHelloInfo(); info != nil {
					return c.SendString(info.ServerName)
				}
				return c.SendString("missing ClientHelloInfo")
			})
			ready <- app
			return app
		}, config.WebConf{
			Name:            "tls-test",
			BodyLimit:       config.WebBodyLimit,
			ServerHttpsAddr: "127.0.0.1:0,127.0.0.1:0",
			CertFile:        certFile,
			KeyFile:         keyFile,
		})
	}()
	app := <-ready
	t.Cleanup(func() {
		if err := app.ShutdownWithTimeout(5 * time.Second); err != nil {
			t.Error(err)
		}
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Error("runOne did not return after shutdown")
		}
	})
	var addrs [2]string
	for i := range addrs {
		select {
		case addrs[i] = <-addresses:
		case <-time.After(5 * time.Second):
			t.Fatal("HTTPS listeners did not start")
		}
		select {
		case disabled := <-banners:
			if !disabled {
				t.Fatal("startup banner must remain disabled")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("startup message hook did not run")
		}
	}
	if setups.Load() != 1 {
		t.Fatalf("setup calls = %d, want 1", setups.Load())
	}

	for range 5 {
		var conns [2]*tls.Conn
		for i, addr := range addrs {
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", addr, &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    roots,
				ServerName: fmt.Sprintf("listener%d.example", i),
			})
			if err != nil {
				t.Fatal(err)
			}
			conns[i] = conn
			t.Cleanup(func() { _ = conn.Close() })
			if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
		}
		for i, conn := range conns {
			if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: close\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			_ = conn.Close()
			if want := fmt.Sprintf("listener%d.example", i); err != nil || string(body) != want {
				t.Fatalf("SNI response = %q, error %v, want %q", body, err, want)
			}
		}
		for range conns {
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("closed connection was not reported")
			}
		}
	}
}

// TestInitTLSConfigError 验证启动证书错误保留路径和标准库错误身份, 不降级为明文.
func TestInitTLSConfigError(t *testing.T) {
	certFile := filepath.Join(t.TempDir(), "missing.pem")
	_, err := initTLSConfig(fiber.New(), certFile, certFile)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), certFile) {
		t.Fatalf("certificate error lost context: %v", err)
	}
}

// writeTestTLSCertificate 为两个测试 SNI 生成独立证书和信任池, 不关闭客户端证书验证.
func writeTestTLSCertificate(t *testing.T) (string, string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pkg-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"listener0.example", "listener1.example"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("test certificate was not loaded")
	}
	return certFile, keyFile, roots
}
