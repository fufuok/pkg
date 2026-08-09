package ntp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"
)

// TestHostPreferredWithLocalServers 使用真实 UDP 报文验证并发查询会忽略无效响应,
// 并从有效响应中选择往返时间最短的 Host.
func TestHostPreferredWithLocalServers(t *testing.T) {
	fastHost, fastDone := startLocalNTPServer(t, 5*time.Millisecond, true)
	slowHost, slowDone := startLocalNTPServer(t, 80*time.Millisecond, true)
	invalidHost, invalidDone := startLocalNTPServer(t, time.Millisecond, false)

	preferred := HostPreferred([]string{slowHost, invalidHost, fastHost})
	if preferred == nil {
		t.Fatal("HostPreferred returned nil for valid local servers")
	}
	if preferred.Host != fastHost {
		t.Fatalf("HostPreferred selected %q, want %q", preferred.Host, fastHost)
	}
	awaitLocalNTPServer(t, fastDone)
	awaitLocalNTPServer(t, slowDone)
	awaitLocalNTPServer(t, invalidDone)
}

// TestHostPreferredWithoutValidResponse 验证所有真实 UDP 响应均无效时返回 nil.
func TestHostPreferredWithoutValidResponse(t *testing.T) {
	invalidHost, invalidDone := startLocalNTPServer(t, 0, false)
	if preferred := HostPreferred([]string{invalidHost}); preferred != nil {
		t.Fatalf("HostPreferred returned unexpected response from %q", preferred.Host)
	}
	awaitLocalNTPServer(t, invalidDone)
}

// startLocalNTPServer 启动只处理一次查询的本地 UDP NTP 服务.
// valid 为 false 时返回无效 stratum, 用于验证 GetResponse 的过滤边界.
func startLocalNTPServer(t *testing.T, delay time.Duration, valid bool) (string, <-chan error) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen local NTP server: %v", err)
	}
	done := make(chan error, 1)
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		done <- serveLocalNTPQuery(conn, delay, valid)
		close(done)
	}()
	return conn.LocalAddr().String(), done
}

// startLocalNTPBlackholeServer 启动只接收查询但不响应的本地 UDP 服务.
//
// received 证明客户端请求已经进入服务器, 避免把写入前 deadline 误判为读取超时.
// stop 会解除服务器等待, 并可由测试 cleanup 重复调用.
func startLocalNTPBlackholeServer(t *testing.T) (string, <-chan struct{}, context.CancelFunc, <-chan error) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen local NTP blackhole server: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	received := make(chan struct{})
	done := make(chan error, 1)
	t.Cleanup(func() {
		stop()
		_ = conn.Close()
	})
	go func() {
		done <- serveLocalNTPBlackhole(ctx, conn, received)
		close(done)
	}()
	return conn.LocalAddr().String(), received, stop, done
}

// serveLocalNTPBlackhole 读取一条完整查询后等待取消, 故意不发送响应.
func serveLocalNTPBlackhole(ctx context.Context, conn *net.UDPConn, received chan<- struct{}) error {
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("set local NTP blackhole deadline: %w", err)
	}
	buffer := make([]byte, 512)
	if _, _, err := conn.ReadFromUDP(buffer); err != nil {
		return fmt.Errorf("read local NTP blackhole query: %w", err)
	}
	close(received)
	<-ctx.Done()
	return nil
}

// serveLocalNTPQuery 读取客户端查询并回写与请求 TransmitTime 匹配的 NTP 响应.
// delay 直接体现在客户端观测到的 RTT 中, 用于验证 HostPreferred 的选择语义.
func serveLocalNTPQuery(conn *net.UDPConn, delay time.Duration, valid bool) error {
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("set local NTP deadline: %w", err)
	}
	buffer := make([]byte, 512)
	n, remote, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return fmt.Errorf("read local NTP query: %w", err)
	}
	var request header
	if err := binary.Read(bytes.NewReader(buffer[:n]), binary.BigEndian, &request); err != nil {
		return fmt.Errorf("decode local NTP query: %w", err)
	}

	if delay > 0 {
		time.Sleep(delay)
	}
	responseAt := time.Now()
	response := header{
		Stratum:        1,
		ReferenceTime:  toNtpTime(responseAt.Add(-time.Second)),
		OriginTime:     request.TransmitTime,
		ReceiveTime:    toNtpTime(responseAt),
		TransmitTime:   toNtpTime(responseAt),
		RootDelay:      ntpTimeShort(1),
		RootDispersion: ntpTimeShort(1),
	}
	response.setVersion(defaultNtpVersion)
	response.setMode(server)
	response.setLeap(LeapNoWarning)
	if !valid {
		response.Stratum = maxStratum
	}

	var encoded bytes.Buffer
	if err := binary.Write(&encoded, binary.BigEndian, &response); err != nil {
		return fmt.Errorf("encode local NTP response: %w", err)
	}
	if _, err := conn.WriteToUDP(encoded.Bytes(), remote); err != nil {
		return fmt.Errorf("write local NTP response: %w", err)
	}
	return nil
}

// awaitLocalNTPServer 等待本地服务完成, 将协议或网络错误归入当前测试.
func awaitLocalNTPServer(t *testing.T, done <-chan error) {
	t.Helper()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
