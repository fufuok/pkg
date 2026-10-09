package tproxy

import (
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/proxy"
	"github.com/valyala/fasthttp"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

const (
	HeaderXProxyClientIP = "X-Proxy-ClientIP"
	HeaderXProxyToken    = "X-Proxy-Token" // #nosec G101
	HeaderXProxyTime     = "X-Proxy-Time"
	HeaderXProxyPass     = "X-Proxy-Pass"
)

// upstreamClient 在实际发送前固定配置的上游协议, 不沿用客户端入站连接的 TLS 状态.
// 嵌入 HostClient 保留连接管理、超时和原有负载均衡接口.
type upstreamClient struct {
	*fasthttp.HostClient
	scheme string
}

// DoDeadline 在 Balancer 重解析请求 URI 后设置协议, 避免提前设置的 scheme 被覆盖.
func (client *upstreamClient) DoDeadline(req *fasthttp.Request, resp *fasthttp.Response, deadline time.Time) error {
	req.URI().SetScheme(client.scheme)
	return client.HostClient.DoDeadline(req, resp, deadline)
}

// ProxyHandler 为管理员配置的固定可信 HTTP(S) 上游创建代理.
//
// 支持 IPv4/IPv6 和私网上游, 不使用 Fiber 全局目标限制策略; backendServers 不得
// 来自客户端请求. 裸 host:port 仍按 HTTP 处理, 无显式端口时使用协议默认端口.
// backendHost 为空时保留原请求 Host, 非空时使用构造期快照; 请求路径和查询串透传.
// 空列表、无法解析的 URL、非 HTTP(S) 或缺主机在构造时 panic.
// 拨号和传输错误清空残留响应后返回原错误, 由调用方决定状态码和回包格式.
// HTTPS 保持证书校验, 不跟随上游重定向.
//
// 示例:
//
//	backendHost := "upstream.example:8080"
//	backendServers := []string{"http://upstream.example:8080"}
//	proxyHandler := ProxyHandler(backendServers, 1*time.Second, backendHost)
//	app.Use("/es/", proxyHandler)
func ProxyHandler(backendServers []string, timeout time.Duration, backendHost string) fiber.Handler {
	if len(backendServers) == 0 {
		panic("tproxy: backend servers cannot be empty")
	}
	clients := make([]fasthttp.BalancingClient, 0, len(backendServers))
	for _, server := range backendServers {
		// 保留原 Balancer 的裸地址兼容, 只解析固定目标, 不在构造期依赖 DNS.
		address := strings.TrimSpace(server)
		if !strings.Contains(address, "://") {
			address = "http://" + address
		}
		upstream, err := url.Parse(address)
		if err != nil {
			panic(fmt.Errorf("tproxy: parse upstream: %w", err))
		}
		if (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Hostname() == "" {
			panic(fmt.Errorf("tproxy: invalid HTTP(S) upstream %q", server))
		}
		clients = append(clients, &upstreamClient{
			HostClient: &fasthttp.HostClient{
				Addr:                     fasthttp.AddMissingPort(upstream.Host, upstream.Scheme == "https"),
				IsTLS:                    upstream.Scheme == "https",
				TLSConfig:                &tls.Config{MinVersion: tls.VersionTLS12},
				MaxConns:                 proxy.ConfigDefault.MaxConnsPerHost,
				NoDefaultUserAgentHeader: true,
				DisablePathNormalizing:   true,
				DialDualStack:            true,
			},
			scheme: upstream.Scheme,
		})
	}
	// 自管固定目标拨号, 此策略只固定 Balancer 的 HTTP 头清理行为.
	policy := proxy.DefaultSecurityPolicy()
	proxyHandler := proxy.Balancer(proxy.Config{
		Client:         &fasthttp.LBClient{Clients: clients, Timeout: timeout},
		SecurityPolicy: &policy,
		ModifyRequest: func(c fiber.Ctx) error {
			if backendHost != "" {
				c.Request().Header.Set("Host", backendHost)
			}
			SetClientIP(c)
			return nil
		},
	})
	return func(c fiber.Ctx) error {
		if err := proxyHandler(c); err != nil {
			// 读体中断可能残留上游响应头和正文, 不能让调用方的错误回包继承它们.
			c.Response().Reset()
			return err
		}
		return nil
	}
}

// SetClientIP 首个代理加密客户端 IP, 中间代理透传
// 当前来访为内网 IP 会跳过设置
// immutable
func SetClientIP(c fiber.Ctx) {
	xip := c.Get(HeaderXProxyClientIP)
	if xip == "" {
		xip = c.IP()
		if !utils.IsInternalIPv4String(xip) {
			xtime := common.GTimeNowString(time.RFC3339)
			xtoken := xhash.HashString(xip, xtime, config.WebTokenSalt)
			c.Request().Header.Set(HeaderXProxyClientIP, xip)
			c.Request().Header.Set(HeaderXProxyToken, xtoken)
			c.Request().Header.Set(HeaderXProxyTime, xtime)
		}
	}
}

// GetClientIP 获取客户端 IP
// 1. 上下文存储中获取
// 2. 下游代理头: 令牌仍是 HashString(xip, xtime, WebTokenSalt), 且 xtime 落在 SignTTL 闭区间内
// 3. TCP 协议 RemoteIP(), 空则 0.0.0.0
// immutable
func GetClientIP(c fiber.Ctx) string {
	clientIP, _ := c.Locals(HeaderXProxyClientIP).(string)
	if clientIP != "" {
		return clientIP
	}

	xip := c.Get(HeaderXProxyClientIP)
	if xip != "" {
		xtoken := c.Get(HeaderXProxyToken)
		xtime := c.Get(HeaderXProxyTime)
		if validProxyClientIP(xip, xtoken, xtime) {
			c.Locals(HeaderXProxyClientIP, xip)
			return xip
		}
	}

	clientIP = c.IP()
	if clientIP == "" {
		clientIP = "0.0.0.0"
	}
	c.Locals(HeaderXProxyClientIP, clientIP)
	return clientIP
}

// validProxyClientIP 校验下游代理头: 令牌仍是 HashString(xip, xtime, WebTokenSalt),
// 且 xtime 必须是 RFC3339, 落在 GTimeNow 前后 SignTTL 的闭区间内.
// SignTTL<=0 或 Config 未就绪时回退 WebSignTTLDefault, 避免热加载空窗把合法近时令牌全部打回.
func validProxyClientIP(xip, xtoken, xtime string) bool {
	if xip == "" || xtoken == "" || xtime == "" {
		return false
	}
	if xtoken != xhash.HashString(xip, xtime, config.WebTokenSalt) {
		return false
	}
	issued, err := time.Parse(time.RFC3339, xtime)
	if err != nil {
		return false
	}
	ttl := int64(config.WebSignTTLDefault)
	if cfg := config.Config(); cfg != nil && cfg.WebConf.SignTTL > 0 {
		ttl = cfg.WebConf.SignTTL
	}
	now := common.GTimeNow()
	window := time.Duration(ttl) * time.Second
	// 用 now±TTL 做闭区间比较, 避免极远未来时间让 Time.Sub 饱和为 MinInt64,
	// 取绝对值后仍为负并误判在窗口内.
	earliest := now.Add(-window)
	latest := now.Add(window)
	return !issued.Before(earliest) && !issued.After(latest)
}
