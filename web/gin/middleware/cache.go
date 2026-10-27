package middleware

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/web/internal/httpcache"
)

// CacheConfig 控制单个中间件实例的本地缓存预算和额外请求头分区.
// 零值使用 256 项, 256KiB 响应和 64KiB 请求体上限; Vary 追加到内置分区头.
type CacheConfig = httpcache.Config

// CacheByRequestURI 缓存无请求体的公开 GET, 完整保留查询参数的原始顺序和编码.
// TTL 非正数使用 10 分钟. 认证, 授权和每请求头中间件必须位于缓存之前;
// gzip 等改写 ResponseWriter 的中间件应位于缓存之后, 使缓存记录实际发送的字节;
// 若注册在缓存外层, 已设置 Content-Encoding 的请求会全部绕过缓存.
// 命中会终止下游链; HEAD, 私有请求, 流式响应和非 200 响应直接透传.
// 应用须使用 Header.Set/Add/Del 操作规范头名, 不直接写入非规范大小写的 Header map 键.
func CacheByRequestURI(ttl time.Duration, configs ...CacheConfig) gin.HandlerFunc {
	return newResponseCache(ttl, false, configs...)
}

// CacheByRawData 缓存显式声明为只读查询的 POST, 键包含完整 URI 和原始请求体摘要.
// 不得用于写操作, 支付或有副作用的接口; 命中不会再次执行业务.
// 请求体超限或读取失败时恢复已读前缀并透传, 不改变业务读取和关闭原正文的行为.
func CacheByRawData(ttl time.Duration, configs ...CacheConfig) gin.HandlerFunc {
	return newResponseCache(ttl, true, configs...)
}

// newResponseCache 为每次构造创建独立有界缓存, 合并同键回源并只共享准入成功的响应.
// 领头响应未准入时进入短暂的 hit-for-pass 窗口, 同键请求不再排队而是并发执行业务.
func newResponseCache(ttl time.Duration, withBody bool, configs ...CacheConfig) gin.HandlerFunc {
	if len(configs) > 1 {
		panic("http cache accepts at most one config")
	}
	var cfg CacheConfig
	if len(configs) == 1 {
		cfg = configs[0]
	}
	store := httpcache.New(ttl, cfg)
	return func(c *gin.Context) {
		req := ginCacheRequest(c.Request)
		if !ginCacheEligible(c, req, withBody, store) {
			c.Next()
			return
		}
		var body []byte
		if withBody {
			var ok bool
			body, ok = readCacheRequestBody(c.Request, store.Config.MaxRequestBodyBytes)
			// 未声明的请求 trailer 只在正文读到 EOF 时才由 net/http 合并进 Request.Trailer,
			// 准入阶段看不到, 预读完成后必须复查; 带 trailer 的请求既不命中也不写入缓存.
			if !ok || len(c.Request.Trailer) > 0 {
				c.Next()
				return
			}
		}
		key := store.Key(req, body)
		if entry, ok := store.Get(key); ok {
			replayGinCache(c, entry)
			return
		}
		// 以当前上下文标识持锁者: HandleContext 或重复注册使同一请求再次进入时直通, 不在自己持有的锁上等待.
		release := store.Acquire(key, c)
		// 取消状态以进入缓存时的请求上下文为准: 内层中间件替换的派生上下文会在其返回时被 cancel, 不代表客户端断开.
		// 客户端断开 (context.Canceled) 不说明响应不可缓存, 放弃领头且不登记直通, 由下一位等待者接任;
		// 超时等其他结果照常按未准入处理.
		ctx := c.Request.Context()
		defer func() { release(errors.Is(ctx.Err(), context.Canceled)) }()
		// 等待期间可能已完成回源, 取消的请求不参与写入或回放. 明确返回 503 而不是仅 Abort,
		// 否则 Gin 会在处理链结束时补写空 200, 让外层日志和统计误判为成功.
		if ctx.Err() != nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if entry, ok := store.Get(key); ok {
			replayGinCache(c, entry)
			return
		}
		writer := &cacheResponseWriter{ResponseWriter: c.Writer, store: store, limit: store.Config.MaxBodyBytes}
		// 外层在业务之前已设置的头不进入条目, 命中时由外层为当前请求重新设置, 避免回放按访客条件设置的值.
		if header := c.Writer.Header(); len(header) > 0 {
			writer.outer = header.Clone()
		}
		c.Writer = writer
		defer func() { c.Writer = writer.ResponseWriter }()
		c.Next()
		writer.storeResponse(c, ctx, key)
	}
}

// ginCacheRequest 取原始请求目标, 路由实际使用的路径与查询和所有同名头, 不规范化查询串或信任转发 Host.
func ginCacheRequest(r *http.Request) httpcache.Request {
	// net/http 已规范化网络输入; 应用也须用 Header.Set/Add/Del, 此处只读借用全部字段行.
	headers := r.Header
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	uri := r.RequestURI
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	return httpcache.Request{
		// 代理头中间件可能按 X-Forwarded-Scheme 等改写 URL.Scheme, 业务按它生成内容时键必须随之分区.
		Method: r.Method, Scheme: scheme, EffectiveScheme: r.URL.Scheme, Host: r.Host, URI: uri,
		Path: r.URL.Path, Query: r.URL.RawQuery, Header: headers,
	}
}

// ginCacheEligible 在读取命中前检查请求及外层中间件已经确定的响应限制.
func ginCacheEligible(c *gin.Context, req httpcache.Request, withBody bool, store *httpcache.Cache) bool {
	method := http.MethodGet
	if withBody {
		method = http.MethodPost
	}
	if req.Method != method || httpcache.SkipRequest(req) || len(c.Request.Trailer) > 0 || c.Writer.Written() ||
		c.Request.Context().Err() != nil {
		return false
	}
	// 外层编码会改变响应表示; 检查全部字段行, 避免空首值遮蔽后续编码.
	for _, value := range c.Writer.Header().Values("Content-Encoding") {
		if value != "" {
			return false
		}
	}
	// 服务端请求的 Body 始终非 nil: HTTP/2 无正文 GET 也是非 NoBody 的流对象, 不能据此判断有无正文.
	// 长度为 0 且没有传输编码才是确定的空正文; 声明长度 (>0) 或未知长度 (-1) 以及 chunked 均透传.
	if !withBody && (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		return false
	}
	return store.CanServe(c.Writer.Header(), c.Writer.Status())
}

// replayGinCache 回放正文及端到端元数据, 保留外层本次请求已经明确设置的非表示策略.
func replayGinCache(c *gin.Context, entry *httpcache.Entry) {
	header := c.Writer.Header()
	// 热命中跳过业务, 必须清除冷请求本会删除的默认表示头.
	// Content-Length 由最终 writer 根据实际输出定界, 不能用缓存正文长度覆盖外层变换.
	for name := range header {
		if httpcache.IsRepresentationHeader(name) {
			delete(header, name)
		}
	}
	// 条目头只读共享, 值复制进本次响应独占的一个底层数组 (末格留给 Age), 各字段为满容量切片:
	// 下游 append 会另起数组, 原地改写也只影响本次响应, 不污染缓存条目, 且不再构造中间头 map.
	n := 1
	for _, values := range entry.Header {
		n += len(values)
	}
	backing := make([]string, n)
	for name, values := range entry.Header {
		// 表示头必须与缓存正文一致; 其他端到端头由外层当前请求已设置的值优先.
		if !httpcache.IsRepresentationHeader(name) {
			if _, exists := header[name]; exists {
				continue
			}
		}
		k := copy(backing, values)
		header[name] = backing[:k:k]
		backing = backing[k:]
	}
	backing[0] = entry.Age()
	header["Age"] = backing[:1:1]
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(entry.Body); err != nil {
		_ = c.Error(err)
	}
	c.Abort()
}

// readCacheRequestBody 有界预读并始终恢复原始字节, Close 仍由原始正文负责.
func readCacheRequestBody(r *http.Request, limit int) ([]byte, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true
	}
	if r.ContentLength > int64(limit) {
		return nil, false
	}
	original := r.Body
	// 剩余原流可能不再重报已返回过的错误, 因此将该次错误也插回前缀之后.
	readLimit := int64(limit)
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	body, err := io.ReadAll(io.LimitReader(original, readLimit))
	readers := []io.Reader{bytes.NewReader(body)}
	if err != nil {
		readers = append(readers, &cacheBodyError{err: err})
	}
	readers = append(readers, original)
	r.Body = &cacheRequestBody{Reader: io.MultiReader(readers...), Closer: original}
	return body, err == nil && len(body) <= limit
}

// cacheBodyError 仅重放一次预读错误, 保留原始错误身份及后续继续读取的可能.
type cacheBodyError struct {
	err error
}

// Read 不伪造任何正文; 错误已交还后用 EOF 让 MultiReader 继续原流.
func (r *cacheBodyError) Read([]byte) (int, error) {
	if r.err == nil {
		return 0, io.EOF
	}
	err := r.err
	r.err = nil
	return 0, err
}

// cacheRequestBody 让已预读前缀和原始流共享原 Close, 不提前关闭上传连接.
type cacheRequestBody struct {
	io.Reader
	io.Closer
}

// cacheResponseWriter 边透传边有界记录, 不改变刷新和连接劫持能力.
type cacheResponseWriter struct {
	gin.ResponseWriter
	store    *httpcache.Cache
	body     bytes.Buffer
	entry    *httpcache.Entry
	outer    http.Header
	limit    int
	disabled bool
}

// storeResponse 只在下游完整成功返回后存储响应, 不把错误, 客户端取消 (ctx 为进入缓存时的请求上下文)
// 或延后声明的 trailer 当完整结果.
// 业务读完正文后才合并出的请求 trailer 可能影响结果, 同样不保存.
// 普通头采用首次提交快照, 避免业务提交后修改的无效头影响真实响应语义.
func (w *cacheResponseWriter) storeResponse(c *gin.Context, ctx context.Context, key [32]byte) {
	// 快照取自业务首次写出之前; 外层在提交时加入的 Set-Cookie, 私有指令或之后声明的 trailer
	// 只出现在实时头中, 存储前再以实时头复核一次, 已可见的不可共享状态即放弃.
	header := w.Header()
	if w.disabled || w.Status() != http.StatusOK || len(c.Errors) > 0 || c.IsAborted() || ctx.Err() != nil ||
		len(c.Request.Trailer) > 0 || !w.store.CanServe(header, w.Status()) || !declaredLength(header, w.body.Len()) {
		return
	}
	entry := w.entry
	// 无快照表示尚未经过捕获路径提交, 或 HandleContext 等绕过了记录. 不在收尾补做 Prepare,
	// 避免 Gin 自动提交时才出现的 Cookie/私有指令漏检, 也不提前提交改变外层收尾语义.
	// 已有表示头发生变化时同样拒绝: 缓冲型外层可能以最终头提交, 快照已不能描述实际响应.
	if entry == nil || !sameRepresentation(entry.Header, header) {
		return
	}
	entry.Body = w.body.Bytes()
	w.store.Set(key, entry)
}

// declaredLength 检查业务声明的 Content-Length 与记录的正文一致. 写出中途失败且忽略错误时客户端得到截断响应,
// 命中由最终 writer 按缓存正文重新定界会把前缀伪装成完整结果; 外层提交时删除的声明不再约束.
func declaredLength(header http.Header, n int) bool {
	values := header["Content-Length"]
	if len(values) == 0 {
		return true
	}
	length := strconv.Itoa(n)
	for _, value := range values {
		if value != length {
			return false
		}
	}
	return true
}

// sameRepresentation 检查首次写出时快照的表示头在处理结束时未被改写. 只比较快照中已有的字段:
// 外层 writer 提交时才补充的编码, 类型等字段不属于记录的正文, 命中时由外层再次生成.
func sameRepresentation(stored, live http.Header) bool {
	for name, values := range stored {
		if httpcache.IsRepresentationHeader(name) && !slices.Equal(values, live[name]) {
			return false
		}
	}
	return true
}

// Write 只记录成功发送的字节; 达到预算后释放记录并继续原响应.
// 头快照与年龄起点都取自底层写出之前: 记录的是写出前的正文, 外层 writer 在提交时才加入的
// Content-Encoding 或逐访客头不属于该正文; 慢连接或阻塞的外层写出耗时计入 TTL.
func (w *cacheResponseWriter) Write(data []byte) (int, error) {
	w.captureHeader()
	n, err := w.ResponseWriter.Write(data)
	if err != nil || n != len(data) {
		w.disabled = true
	}
	w.captureBody(data[:n])
	return n, err
}

// WriteString 复用字节写入路径, 避免字符串输出绕过缓存和预算检查.
func (w *cacheResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

// WriteHeaderNow 在头真正提交前拍快照, 不缓存外层提交时加入或业务提交后才修改的头.
func (w *cacheResponseWriter) WriteHeaderNow() {
	w.captureHeader()
	w.ResponseWriter.WriteHeaderNow()
}

// captureHeader 只保存首次提交前的头并记录年龄起点; 实际可保存的字段由共享内核筛选.
func (w *cacheResponseWriter) captureHeader() {
	if w.entry == nil && !w.disabled {
		// 准入检查先于复制, 超大头或私有响应不额外保留完整快照和正文.
		entry, ok := w.store.Prepare(w.Header(), nil, w.Status())
		if !ok {
			w.disabled = true
			return
		}
		entry.OmitInherited(w.outer)
		entry.StoredAt = time.Now()
		w.entry = entry
	}
}

// captureBody 超限后停止累积, 避免大下载按响应长度占用额外内存.
func (w *cacheResponseWriter) captureBody(data []byte) {
	if w.disabled || len(data) > w.limit-w.body.Len() {
		w.disabled = true
		w.body = bytes.Buffer{}
		return
	}
	_, _ = w.body.Write(data)
}

// Flush 表示调用方需要流式语义, 因而禁止保存整个响应.
func (w *cacheResponseWriter) Flush() {
	w.disabled = true
	w.body = bytes.Buffer{}
	w.ResponseWriter.Flush()
}

// Hijack 将连接所有权交给调用方, 不把升级连接写入普通响应缓存.
func (w *cacheResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.disabled = true
	return w.ResponseWriter.Hijack()
}

// Pusher 保留 HTTP/2 能力, 带主动推送的处理不作为普通完整响应缓存.
func (w *cacheResponseWriter) Pusher() http.Pusher {
	w.disabled = true
	return w.ResponseWriter.Pusher()
}

// Unwrap 允许标准库 ResponseController 继续查找底层 writer 的扩展能力.
// 交出底层 writer 后调用方可能绕过记录写出, 无论先后顺序, 缓冲的正文都可能不完整,
// 因此与 Flush, Hijack 一样不再保存本次响应; 经 ResponseController 设置截止时间的处理同样不缓存.
func (w *cacheResponseWriter) Unwrap() http.ResponseWriter {
	w.disabled = true
	w.body = bytes.Buffer{}
	return w.ResponseWriter
}
