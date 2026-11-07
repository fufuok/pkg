package middleware

import (
	"context"
	"errors"
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
		// 入口上下文在 Acquire 与 Next 之前读取, 内层中间件替换并在返回时 cancel 的派生上下文不影响它;
		// 外层同步期限已到时等待者可立即脱离键锁, 执行结果可能不完整故同样不保存.
		entryCtx := c.Context()
		// 以当前上下文标识持锁者: RestartRouting 或重复注册使同一请求再次进入时直通, 不在自己持有的锁上等待.
		// 未执行业务就离开或入口上下文被主动取消时按放弃释放, 不登记直通窗口; 执行超时等未准入结果照常开启直通.
		release := store.AcquireContext(entryCtx, key, c)
		executed := false
		defer func() { release(!executed || errors.Is(entryCtx.Err(), context.Canceled)) }()
		if entryCtx.Err() != nil {
			return c.Next()
		}
		if entry, ok := store.Get(key); ok {
			return replyWithFiberCache(c, entry)
		}
		// 外层在业务之前已设置的非表示头不进入条目, 命中时由外层为当前请求重新设置, 避免回放按访客条件设置的值.
		// 表示头 (如 fasthttp 默认 Content-Type) 不参与 OmitInherited, 仅存在非表示头时才克隆独立快照.
		outer := fiberOuterNonRepHeaders(c)

		executed = true
		err := c.Next()
		// 缓存运行在请求协程, 内层超时标记由同一协程写入, 读取安全. 真实截止后工作协程仍可能访问 Ctx,
		// 此时只返回原错误, 不读取活动请求或响应; OnTimeout 写出兜底并返回 nil 也由标记识别.
		if err != nil || entryCtx.Err() != nil || c.RequestCtx().LastTimeoutErrorResponse() != nil {
			return err
		}
		storeFiberCacheResponse(c, store, key, outer)
		return nil
	}
}

// storeFiberCacheResponse 在请求未劫持、非流式且通过准入扫描时将 200 响应写入缓存.
func storeFiberCacheResponse(c fiber.Ctx, store *httpcache.Cache, key [32]byte, outer http.Header) {
	// Body() 会消费 fasthttp 的响应流; SendStream, SSE 和 SendFile 必须先在此退出.
	// 接管连接后真实输出由 hijack 处理器负责, 响应对象可能根本不发送 (HijackSetNoResponse),
	// 缓存它会向后续请求回放源站没有发出的内容.
	if c.RequestCtx().Hijacked() || c.Response().IsBodyStream() || c.Response().SkipBody {
		return
	}
	status := c.StatusCode()
	body := c.Response().Body()
	if status != fiber.StatusOK || len(body) > store.Config.MaxBodyBytes {
		return
	}
	// 先在原始响应头上零分配完成完整准入扫描, 使 no-store, private, Set-Cookie 等直通响应不分配 http.Header.
	var respScan httpcache.ResponseScan
	for name, value := range c.Response().Header.All() {
		if !store.ScanResponseHeader(name, value, &respScan) {
			return
		}
	}
	if entry, ok := store.Prepare(fiberCacheResponseHeaders(c), body, status); ok {
		entry.OmitInherited(outer)
		store.Set(key, entry)
	}
}

// fiberCacheRequestKey 检查方法, 请求体, 凭据和上游隐私策略, 只为能够参与缓存的请求生成键.
// 请求流直接返回, 原始正文在业务处理前只做有界同步哈希, 不触发 Fiber 的自动解压或消费.
func fiberCacheRequestKey(c fiber.Ctx, store *httpcache.Cache, withBody bool) ([32]byte, bool) {
	method := c.Method()
	if (!withBody && method != fiber.MethodGet) || (withBody && method != fiber.MethodPost) || c.Request().IsBodyStream() {
		return [32]byte{}, false
	}
	body := c.Request().Body()
	if (!withBody && len(body) > 0) || len(body) > store.Config.MaxRequestBodyBytes {
		return [32]byte{}, false
	}
	keyHeaderCount, ok := scanFiberRequestHeaders(c, store)
	if !ok || !fiberCacheInitialResponse(c, store) {
		return [32]byte{}, false
	}
	var requestHeaders http.Header
	if keyHeaderCount > 0 {
		var stackBacking [8]string
		requestHeaders = collectFiberKeyHeaders(c, store, keyHeaderCount, stackBacking[:0])
	}
	req := httpcache.Request{
		Method:        method,
		Scheme:        c.Scheme(),
		Host:          utils.B2S(c.Request().Header.Host()),
		EffectiveHost: c.Host(),
		URI:           c.OriginalURL(),
		Path:          c.Path(),
		Query:         utils.B2S(c.Request().URI().QueryString()),
		Header:        requestHeaders,
	}
	// Key 同步消费正文和零拷贝字段, 在下游改写或框架回收缓冲区之前完成哈希, 无需额外复制.
	return store.Key(req, body), true
}

// scanFiberRequestHeaders 在原始请求头上零分配完成跳过检查, 并返回参与键计算的字段行数.
func scanFiberRequestHeaders(c fiber.Ctx, store *httpcache.Cache) (int, bool) {
	count := 0
	for name, value := range c.Request().Header.All() {
		_, inKey, skip := store.InspectRequestHeader(name, value)
		if skip {
			return 0, false
		}
		if inKey {
			count++
		}
	}
	return count, true
}

// collectFiberKeyHeaders 仅收集参与缓存键计算的请求头字段行, 常规数量复用调用方栈缓冲.
func collectFiberKeyHeaders(c fiber.Ctx, store *httpcache.Cache, count int, backing []string) http.Header {
	var headers http.Header
	if count <= cap(backing) {
		headers = make(http.Header)
	} else {
		headers = make(http.Header, count)
		backing = make([]string, 0, count)
	}
	for name, value := range c.Request().Header.All() {
		canonical, inKey, _ := store.InspectRequestHeader(name, value)
		if !inKey {
			continue
		}
		addFiberCacheHeader(headers, &backing, canonical, utils.B2S(value))
	}
	return headers
}

// fiberCacheInitialResponse 保留上游已经形成的状态, 正文和流, 命中不能覆盖当前请求的响应策略.
// 流式检查先于 Body, 隐私和变化头直接在原始响应头上零分配扫描校验.
func fiberCacheInitialResponse(c fiber.Ctx, store *httpcache.Cache) bool {
	if c.Response().IsBodyStream() || c.Response().SkipBody || c.StatusCode() != fiber.StatusOK ||
		len(c.Response().Body()) > 0 {
		return false
	}
	var scan httpcache.ResponseScan
	for name, value := range c.Response().Header.All() {
		if !store.ScanResponseHeader(name, value, &scan) {
			return false
		}
	}
	return true
}

// fiberOuterNonRepHeaders 仅在外层已预设非表示响应头时复制独立快照供 OmitInherited 比对;
// 只有 fasthttp 默认 Content-Type 等表示头时零分配返回 nil.
func fiberOuterNonRepHeaders(c fiber.Ctx) http.Header {
	count := 0
	for name := range c.Response().Header.All() {
		if !httpcache.IsRepresentationHeader(http.CanonicalHeaderKey(utils.B2S(name))) {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	headers := make(http.Header, count)
	backing := make([]string, 0, count)
	for name, value := range c.Response().Header.All() {
		key := http.CanonicalHeaderKey(utils.B2S(name))
		if !httpcache.IsRepresentationHeader(key) {
			addFiberCacheHeader(headers, &backing, strings.Clone(key), string(value))
		}
	}
	return headers
}

// fiberCacheResponseHeaders 把响应头零拷贝映射为规范名 http.Header 供 Prepare 入库,
// Prepare 内部在保存条目时会对名称与值做独立字符串克隆以隔离 fasthttp 缓冲区.
func fiberCacheResponseHeaders(c fiber.Ctx) http.Header {
	total := c.Response().Header.Len()
	if total == 0 {
		return nil
	}
	headers := make(http.Header, total)
	backing := make([]string, 0, total)
	for name, value := range c.Response().Header.All() {
		addFiberCacheHeader(headers, &backing, http.CanonicalHeaderKey(utils.B2S(name)), utils.B2S(value))
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

// replyWithFiberCache 回放正文及端到端元数据, 命中响应与同条件冷请求一致.
// 条目只含业务设置或改写过的头 (与外层相同的值已由 OmitInherited 剔除), 冷请求中业务后执行会覆盖外层同名头,
// 因此回放时条目头优先; 业务未触及的外层头不在条目中, 命中时保留外层本次请求设置的值.
// SetBody 会复制缓存正文, 避免后续压缩或业务中间件修改 Fiber 缓冲区时污染共享条目.
func replyWithFiberCache(c fiber.Ctx, entry *httpcache.Entry) error {
	// fasthttp 的 All 迭代中不能修改头集合, 先复制实际名称再清除旧表示元数据.
	// 缓存条目中缺失的表示头也必须清除, 不能重新露出冷请求业务已删除的默认值.
	var buf [4]string
	representationNames := buf[:0]
	for name := range c.Response().Header.All() {
		if httpcache.IsRepresentationHeader(utils.B2S(name)) {
			representationNames = append(representationNames, string(name))
		}
	}
	for _, name := range representationNames {
		c.Response().Header.Del(name)
	}
	// Add 自行复制字段字节, 直接只读遍历共享头, 省去中间 map 和值切片克隆.
	for name, values := range entry.Header {
		// 表示头必须与缓存正文一致; 其他条目头是业务显式结果, 同样覆盖外层同名值, 避免命中时放宽业务收紧的策略.
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
