package tproxy

import (
	"time"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/proxy"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

const (
	HeaderXProxyClientIP = "X-Proxy-ClientIP"
	HeaderXProxyToken    = "X-Proxy-Token" // #nosec G101
	HeaderXProxyTime     = "X-Proxy-Time"
	HeaderXProxyPass     = "X-Proxy-Pass"
)

// ProxyHandler 请求代理
//
// 示例:
//
//	backendHost := "1.2.3.4:555"
//	backendServers := []string{"http://1.2.3.4:555"}
//	proxyHandler := ProxyHandler(backendServers, 1*time.Second, backendHost)
//	app.Use("/test/ff", proxyHandler)
//	6.6.6.6 请求: http://localhost:666/test/ff -> http://1.2.3.4:555/test/ff
//	在 1.2.3.4 服务中 GetClientIP(c) 能获取到真实客户端请求IP: 6.6.6.6
func ProxyHandler(backendServers []string, timeout time.Duration, backendHost string) fiber.Handler {
	proxyHandler := proxy.Balancer(proxy.Config{
		Servers: backendServers,
		Timeout: timeout,
		ModifyRequest: func(c fiber.Ctx) error {
			if backendHost != "" {
				c.Request().Header.Set("Host", backendHost)
			}
			SetClientIP(c)
			return nil
		},
	})
	return proxyHandler
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
