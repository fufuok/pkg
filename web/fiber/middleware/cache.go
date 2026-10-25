package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/web/internal/httpcache"
)

// CacheConfig 限制单个中间件实例的容量, 正文大小和允许变化的请求头.
// 零值采用安全默认值, 构造后不应修改配置或其中的 Vary 切片.
type CacheConfig = httpcache.Config

// CacheByRequestURI 缓存无正文的 GET 响应, 键包含方法, 协议, Host, 完整 URI 和表示相关请求头.
// 鉴权应先于本中间件运行; 凭据请求, 条件请求, 流式响应和不允许共享的响应自动直通.
// ttl 非正值使用默认有效期, 可选配置最多一项. 缓存仅属于当前实例和当前进程.
func CacheByRequestURI(ttl time.Duration, configs ...CacheConfig) fiber.Handler {
	return newResponseCache(ttl, false, configs...)
}

// CacheByRawData 为应用显式声明的只读 POST 接口缓存响应, 在完整请求键中额外加入原始正文.
// 禁止用于写操作; 不能由缓存层自动判断接口是否有副作用. 超大正文及请求体流保持原样直通.
// 原始正文保持给业务读取, 缓存层不主动解压 Content-Encoding, 也不跨进程共享缓存.
func CacheByRawData(ttl time.Duration, configs ...CacheConfig) fiber.Handler {
	return newResponseCache(ttl, true, configs...)
}

// newResponseCache 将 Fiber 的请求和响应映射至共用策略, 对不可缓存的结果不分享业务执行.
// 同键等待者在获得锁后重新查询, 仅已通过响应检查并成功存入的结果能够被复用;
// 领头响应未准入时进入短暂的 hit-for-pass 窗口, 同键请求不再排队而是并发执行业务.
func newResponseCache(ttl time.Duration, withBody bool, configs ...CacheConfig) fiber.Handler {
	if len(configs) > 1 {
		panic("httpcache: at most one cache configuration is allowed")
	}
	var cfg CacheConfig
	if len(configs) > 0 {
		cfg = configs[0]
	}
	store := httpcache.New(ttl, cfg)
	return func(c fiber.Ctx) error {
		key, ok := fiberCacheRequestKey(c, store, withBody)
		if !ok {
			return c.Next()
		}
		if entry, ok := store.Get(key); ok {
			return replyWithFiberCache(c, entry)
		}
		release := store.Acquire(key)
		defer release()
		if entry, ok := store.Get(key); ok {
			return replyWithFiberCache(c, entry)
		}

		err := c.Next()
		// 真实截止后 worker 仍可能访问 Ctx, 此后只返回原错误, 不读取活动请求或响应.
		if c.RequestCtx().LastTimeoutErrorResponse() != nil || err != nil {
			return err
		}
		// Body() 会消费 fasthttp 的响应流; SendStream, SSE 和 SendFile 必须先在此退出.
		// 接管连接后真实输出由 hijack 处理器负责, 响应对象可能根本不发送 (HijackSetNoResponse),
		// 缓存它会向后续请求回放源站没有发出的内容.
		if c.RequestCtx().Hijacked() || c.Response().IsBodyStream() || c.Response().SkipBody {
			return nil
		}
		if entry, ok := store.Prepare(fiberCacheResponseHeaders(c), c.Response().Body(), c.StatusCode()); ok {
			store.Set(key, entry)
		}
		return nil
	}
}

// fiberCacheRequestKey 检查方法, 请求体, 凭据和上游隐私策略, 只为能够参与缓存的请求生成键.
// 请求流直接返回, 原始正文在业务处理前只做有界同步哈希, 不触发 Fiber 的自动解压或消费.
func fiberCacheRequestKey(c fiber.Ctx, store *httpcache.Cache, withBody bool) ([32]byte, bool) {
	method := c.Method()
	if (!withBody && method != fiber.MethodGet) || (withBody && method != fiber.MethodPost) ||
		c.Request().IsBodyStream() || len(c.Request().Header.Peek("Trailer")) > 0 {
		return [32]byte{}, false
	}
	if !fiberCacheInitialResponse(c, store) {
		return [32]byte{}, false
	}
	requestHeaders := fiberCacheRequestHeaders(c)
	req := httpcache.Request{
		Method:        method,
		Scheme:        c.Scheme(),
		Host:          string(c.Request().Header.Host()),
		EffectiveHost: c.Host(),
		URI:           c.OriginalURL(),
		Header:        func(name string) string { return strings.Join(requestHeaders.Values(name), ",") },
		HeaderValues:  requestHeaders.Values,
	}
	if httpcache.SkipRequest(req) {
		return [32]byte{}, false
	}
	body := c.Request().Body()
	if (!withBody && len(body) > 0) || len(body) > store.Config.MaxRequestBodyBytes {
		return [32]byte{}, false
	}
	// Key 同步消费正文, 在下游改写或框架回收缓冲区之前完成哈希, 无需额外复制.
	return store.Key(req, body), true
}

// fiberCacheInitialResponse 保留上游已经形成的状态, 正文和流, 命中不能覆盖当前请求的响应策略.
// 流式检查先于 Body, 隐私和变化头同样受共用准入规则约束.
func fiberCacheInitialResponse(c fiber.Ctx, store *httpcache.Cache) bool {
	if c.Response().IsBodyStream() || c.Response().SkipBody || c.StatusCode() != fiber.StatusOK ||
		len(c.Response().Body()) > 0 {
		return false
	}
	return store.CanServe(fiberCacheResponseHeaders(c), c.StatusCode())
}

// fiberCacheRequestHeaders 复制并规范化全部请求头, 保留多行值及大小写不规范的敏感字段.
// 不能只取首行, 否则空首行可能掩盖后续 Authorization, Cookie 或 Cache-Control.
func fiberCacheRequestHeaders(c fiber.Ctx) http.Header {
	headers := make(http.Header)
	for name, value := range c.Request().Header.All() {
		headers.Add(string(name), string(value))
	}
	return headers
}

// fiberCacheResponseHeaders 在请求有效期内复制响应头, 让共用策略统一执行隐私和端到端头准入检查.
func fiberCacheResponseHeaders(c fiber.Ctx) http.Header {
	headers := make(http.Header)
	for name, value := range c.Response().Header.All() {
		headers.Add(string(name), string(value))
	}
	return headers
}

// replyWithFiberCache 回放正文及端到端元数据, 保留外层本次请求已经明确设置的非表示策略.
// SetBody 会复制缓存正文, 避免后续压缩或业务中间件修改 Fiber 缓冲区时污染共享条目.
func replyWithFiberCache(c fiber.Ctx, entry *httpcache.Entry) error {
	for name, values := range entry.ReplayHeader() {
		// 默认 Content-Type 等表示头不能覆盖缓存正文的元数据; 其他头保留外层显式值.
		if !httpcache.IsRepresentationHeader(name) && fiberCacheHeaderPresent(c, name) {
			continue
		}
		c.Response().Header.Del(name)
		for _, value := range values {
			c.Response().Header.Add(name, value)
		}
	}
	c.Status(fiber.StatusOK)
	c.Response().SetBody(entry.Body)
	return nil
}

// fiberCacheHeaderPresent 按 HTTP 大小写无关语义检查存在性, 保留外层设置的空字段值.
// 不依赖 Peek 返回非空值, 兼容禁用头名规范化及显式空安全策略的应用.
func fiberCacheHeaderPresent(c fiber.Ctx, name string) bool {
	for current := range c.Response().Header.All() {
		if strings.EqualFold(string(current), name) {
			return true
		}
	}
	return false
}
