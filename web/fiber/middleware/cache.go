package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/web/internal/httpcache"
)

// CacheConfig 限制单个中间件实例的容量, 正文大小和允许变化的请求头.
// 零值采用安全默认值, 构造后不应修改配置或其中的 Vary 切片.
type CacheConfig = httpcache.Config

// CacheByRequestURI 缓存无正文的 GET 响应, 键包含方法, 协议, Host, 完整 URI 和表示相关请求头.
// 鉴权应先于本中间件运行; 凭据请求, 条件请求, 流式响应和不允许共享的响应自动直通.
// ttl 非正值使用默认有效期, 可选配置最多一项. 缓存仅属于当前实例和当前进程.
// 须注册在 pkg WithTimeout 外层; 注册在其内层时整段透传, 不命中也不存储.
// 外置的原生或第三方异步 timeout 不在支持范围, 不根据 context 是否有期限猜测执行位置.
// 禁用头名规范化时请求侧凭据与隐私检查仍有效, 但非规范表示头不保证被清理或替换, 应用须使用规范头名.
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
		// 位于 WithTimeout 内层时缓存运行在超时工作协程, 截止后与请求协程并发访问 Ctx, 读取响应或超时标记
		// 都无法建立先后关系; 只在入口检查一次标记并整段透传, 不命中也不存储.
		if c.Locals(asyncTimeoutKey{}) != nil {
			return c.Next()
		}
		key, ok := fiberCacheRequestKey(c, store, withBody)
		if !ok {
			return c.Next()
		}
		if entry, ok := store.Get(key); ok {
			return replyWithFiberCache(c, entry)
		}
		// 以当前上下文标识持锁者: RestartRouting 或重复注册使同一请求再次进入时直通, 不在自己持有的锁上等待.
		// fasthttp 不因客户端断开取消处理, 释放时从不放弃领头.
		release := store.Acquire(key, c)
		defer release(false)
		if entry, ok := store.Get(key); ok {
			return replyWithFiberCache(c, entry)
		}
		// 外层在业务之前已设置的头不进入条目, 命中时由外层为当前请求重新设置, 避免回放按访客条件设置的值.
		// 业务会改写响应缓冲区, 跨越 Next 的快照必须持有独立副本.
		outer := fiberCacheResponseHeaders(c, true)
		// 入口上下文在 Next 之前读取, 内层中间件替换并在返回时 cancel 的派生上下文不影响它;
		// 外层同步期限已到时结果可能不完整, 同样不保存.
		entryCtx := c.Context()

		err := c.Next()
		// 缓存运行在请求协程, 内层超时标记由同一协程写入, 读取安全. 真实截止后工作协程仍可能访问 Ctx,
		// 此时只返回原错误, 不读取活动请求或响应; OnTimeout 写出兜底并返回 nil 也由标记识别.
		if err != nil || entryCtx.Err() != nil || c.RequestCtx().LastTimeoutErrorResponse() != nil {
			return err
		}
		// Body() 会消费 fasthttp 的响应流; SendStream, SSE 和 SendFile 必须先在此退出.
		// 接管连接后真实输出由 hijack 处理器负责, 响应对象可能根本不发送 (HijackSetNoResponse),
		// 缓存它会向后续请求回放源站没有发出的内容.
		if c.RequestCtx().Hijacked() || c.Response().IsBodyStream() || c.Response().SkipBody {
			return nil
		}
		if entry, ok := store.Prepare(fiberCacheResponseHeaders(c, true), c.Response().Body(), c.StatusCode()); ok {
			entry.OmitInherited(outer)
			store.Set(key, entry)
		}
		return nil
	}
}

// fiberCacheRequestKey 检查方法, 请求体, 凭据和上游隐私策略, 只为能够参与缓存的请求生成键.
// 请求流直接返回, 原始正文在业务处理前只做有界同步哈希, 不触发 Fiber 的自动解压或消费.
func fiberCacheRequestKey(c fiber.Ctx, store *httpcache.Cache, withBody bool) ([32]byte, bool) {
	method := c.Method()
	if (!withBody && method != fiber.MethodGet) || (withBody && method != fiber.MethodPost) || c.Request().IsBodyStream() {
		return [32]byte{}, false
	}
	if !fiberCacheInitialResponse(c, store) {
		return [32]byte{}, false
	}
	requestHeaders := fiberCacheRequestHeaders(c, store)
	req := httpcache.Request{
		Method:        method,
		Scheme:        c.Scheme(),
		Host:          string(c.Request().Header.Host()),
		EffectiveHost: c.Host(),
		URI:           c.OriginalURL(),
		Path:          c.Path(),
		Query:         utils.B2S(c.Request().URI().QueryString()),
		Header:        requestHeaders,
	}
	if httpcache.SkipRequest(req) {
		return [32]byte{}, false
	}
	body := c.Request().Body()
	if (!withBody && len(body) > 0) || len(body) > store.Config.MaxRequestBodyBytes {
		return [32]byte{}, false
	}
	// Key 同步消费正文和零拷贝字段, 在下游改写或框架回收缓冲区之前完成哈希, 无需额外复制.
	return store.Key(req, body), true
}

// fiberCacheInitialResponse 保留上游已经形成的状态, 正文和流, 命中不能覆盖当前请求的响应策略.
// 流式检查先于 Body, 隐私和变化头同样受共用准入规则约束. 检查同步完成, 头映射零拷贝引用响应缓冲区.
func fiberCacheInitialResponse(c fiber.Ctx, store *httpcache.Cache) bool {
	if c.Response().IsBodyStream() || c.Response().SkipBody || c.StatusCode() != fiber.StatusOK ||
		len(c.Response().Body()) > 0 {
		return false
	}
	return store.CanServe(fiberCacheResponseHeaders(c, false), c.StatusCode())
}

// fiberCacheRequestHeaders 只复制跳过检查和请求键需要的请求头, 按规范名保留全部字段行.
// 不能只取首行, 否则空首行可能掩盖后续 Authorization, Cookie 或 Cache-Control; 名称大小写无关匹配,
// 关闭头名规范化时小写凭据同样被识别. User-Agent 等无关字段不复制, 命中路径分配不随请求头数量增长.
func fiberCacheRequestHeaders(c fiber.Ctx, store *httpcache.Cache) http.Header {
	headers := make(http.Header, 4)
	backing := make([]string, 0, 8)
	for name, value := range c.Request().Header.All() {
		if canonical, ok := store.RequestHeaderName(name); ok {
			// 请求头只在业务执行前同步用于跳过检查和计算键, 不进入缓存条目, 可零拷贝引用请求缓冲区.
			addFiberCacheHeader(headers, &backing, canonical, utils.B2S(value))
		}
	}
	return headers
}

// fiberCacheResponseHeaders 把响应头映射为共用策略使用的规范名 http.Header, 统一执行隐私和端到端头准入检查.
// owned 为 false 时零拷贝引用响应缓冲区, 只能在响应被改写前同步使用 (命中前的策略检查);
// 跨越业务处理或存入条目的快照必须传 true 复制名称与值.
func fiberCacheResponseHeaders(c fiber.Ctx, owned bool) http.Header {
	headers := make(http.Header, 4)
	backing := make([]string, 0, 4)
	for name, value := range c.Response().Header.All() {
		key, val := http.CanonicalHeaderKey(utils.B2S(name)), utils.B2S(value)
		if owned {
			key, val = strings.Clone(key), string(value)
		}
		addFiberCacheHeader(headers, &backing, key, val)
	}
	return headers
}

// addFiberCacheHeader 追加一个字段行. 首次出现的名称切出共享底层数组的满容量单元素切片, 一个数组承载全部
// 单行字段, 避免逐字段分配; 重复名称 append 时容量已满会另起数组, 共享数组扩容也只影响之后的字段, 互不覆盖.
func addFiberCacheHeader(headers http.Header, backing *[]string, name, value string) {
	if values, ok := headers[name]; ok {
		headers[name] = append(values, value)
		return
	}
	*backing = append(*backing, value)
	n := len(*backing)
	headers[name] = (*backing)[n-1 : n : n]
}

// replyWithFiberCache 回放正文及端到端元数据, 保留外层本次请求已经明确设置的非表示策略.
// SetBody 会复制缓存正文, 避免后续压缩或业务中间件修改 Fiber 缓冲区时污染共享条目.
func replyWithFiberCache(c fiber.Ctx, entry *httpcache.Entry) error {
	// fasthttp 的 All 迭代中不能修改头集合, 先复制实际名称再清除旧表示元数据.
	// 缓存条目中缺失的表示头也必须清除, 不能重新露出冷请求业务已删除的默认值.
	var buf [4]string
	representationNames := buf[:0]
	for name := range c.Response().Header.All() {
		if httpcache.IsRepresentationHeader(string(name)) {
			representationNames = append(representationNames, string(name))
		}
	}
	for _, name := range representationNames {
		c.Response().Header.Del(name)
	}
	// Add 自行复制字段字节, 直接只读遍历共享头, 省去中间 map 和值切片克隆.
	for name, values := range entry.Header {
		// 默认 Content-Type 等表示头不能覆盖缓存正文的元数据; 其他头保留外层显式值.
		if !httpcache.IsRepresentationHeader(name) && len(c.Response().Header.PeekAll(name)) > 0 {
			continue
		}
		c.Response().Header.Del(name)
		for _, value := range values {
			c.Response().Header.Add(name, value)
		}
	}
	c.Response().Header.Set("Age", entry.Age())
	c.Status(fiber.StatusOK)
	c.Response().SetBody(entry.Body)
	return nil
}
