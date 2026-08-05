package xcrypto

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"time"
)

var (
	ErrInvalidParam = errors.New("invalid parameter")
	ErrInvalidCert  = errors.New("invalid certificate")
)

// GetCertificate 获取 TLS 服务返回的首张证书.
// addr 支持主机名、显式 host:port 和 https:// 前缀; 未指定端口时使用 443.
// tlsConf 为 nil 时使用系统信任根, 调用方需要为私有 CA 显式提供配置.
func GetCertificate(network, addr string, timeout time.Duration, tlsConf *tls.Config) (*x509.Certificate, error) {
	addr, err := normalizeCertificateAddress(addr)
	if err != nil {
		return nil, err
	}

	dialer := new(net.Dialer)
	if timeout > 0 {
		dialer.Timeout = timeout
	}
	if tlsConf == nil {
		tlsConf = new(tls.Config)
	}
	conn, err := tls.DialWithDialer(dialer, network, addr, tlsConf)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
	}()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) > 0 {
		return certs[0], nil
	}
	return nil, ErrInvalidCert
}

// normalizeCertificateAddress 规范化证书查询地址.
// 显式端口保持不变, 裸主机名和无端口 IPv6 地址使用默认 TLS 端口 443.
func normalizeCertificateAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", ErrInvalidParam
	}
	addr = strings.TrimPrefix(addr, "https://")
	// 显式端口必须原样保留, 只有缺少端口时才补 TLS 默认端口.
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), "443")
	}
	return addr, nil
}
