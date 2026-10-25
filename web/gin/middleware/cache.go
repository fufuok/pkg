package middleware

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/web/internal/httpcache"
)

// CacheConfig 控制单个中间件实例的本地缓存预算和额外请求头分区.
// 零值使用 256 项, 256KiB 响应和 64KiB 请求体上限; Vary 追加到内置分区头.
type CacheConfig = httpcache.Config

// CacheByRequestURI 缓存无请求体的公开 GET, 完整保留查询参数的原始顺序和编码.
// TTL 非正数使用 10 分钟. 认证, 授权和每请求头中间件必须位于缓存之前;
// gzip 等改写 ResponseWriter 的中间件应位于缓存之后, 使缓存记录实际发送的字节.
// 命中会终止下游链; HEAD, 私有请求, 流式响应和非 200 响应直接透传.
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
			if !ok {
				c.Next()
				return
			}
		}
		key := store.Key(req, body)
		if entry, ok := store.Get(key); ok {
			replayGinCache(c, entry)
			return
		}
		release := store.Acquire(key)
		defer release()
		// 等待期间可能已完成回源, 取消的请求不参与写入或回放.
		if c.Request.Context().Err() != nil {
			c.Abort()
			return
		}
		if entry, ok := store.Get(key); ok {
			replayGinCache(c, entry)
			return
		}
		writer := &cacheResponseWriter{ResponseWriter: c.Writer, store: store, limit: store.Config.MaxBodyBytes}
		c.Writer = writer
		defer func() { c.Writer = writer.ResponseWriter }()
		c.Next()
		writer.storeResponse(c, key)
	}
}

// ginCacheRequest 取原始请求目标和所有同名头, 不规范化查询串或信任转发 Host.
func ginCacheRequest(r *http.Request) httpcache.Request {
	// 标准请求头只在同步的准入和键计算阶段读取, 不保留到业务执行后, 无需再复制.
	// 应用直接写入非规范键时仍完整归并, 保留小写敏感头和全部同名字段行的检查.
	headers := r.Header
	for name := range headers {
		if name != http.CanonicalHeaderKey(name) {
			headers = make(http.Header, len(r.Header))
			for name, values := range r.Header {
				name = http.CanonicalHeaderKey(name)
				headers[name] = append(headers[name], values...)
			}
			break
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	uri := r.RequestURI
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	return httpcache.Request{
		Method: r.Method, Scheme: scheme, Host: r.Host, URI: uri,
		Header:       func(name string) string { return strings.Join(headers.Values(name), ",") },
		HeaderValues: headers.Values,
	}
}

// ginCacheEligible 在读取命中前检查请求及外层中间件已经确定的响应限制.
func ginCacheEligible(c *gin.Context, req httpcache.Request, withBody bool, store *httpcache.Cache) bool {
	method := http.MethodGet
	if withBody {
		method = http.MethodPost
	}
	if req.Method != method || httpcache.SkipRequest(req) || len(c.Request.Trailer) > 0 || c.Writer.Written() ||
		c.Request.Context().Err() != nil || c.Writer.Header().Get("Content-Encoding") != "" {
		return false
	}
	if !withBody && (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 ||
		(c.Request.Body != nil && c.Request.Body != http.NoBody)) {
		return false
	}
	return store.CanServe(c.Writer.Header(), c.Writer.Status())
}

// replayGinCache 回放正文及端到端元数据, 保留外层本次请求已经明确设置的非表示策略.
func replayGinCache(c *gin.Context, entry *httpcache.Entry) {
	for name, values := range entry.ReplayHeader() {
		// 表示头必须与缓存正文一致; 其他端到端头由外层当前请求已设置的值优先.
		if !httpcache.IsRepresentationHeader(name) && ginCacheHeaderPresent(c.Writer.Header(), name) {
			continue
		}
		c.Writer.Header()[name] = values
	}
	c.Status(http.StatusOK)
	if _, err := c.Writer.Write(entry.Body); err != nil {
		_ = c.Error(err)
	}
	c.Abort()
}

// ginCacheHeaderPresent 保留外层显式空值, 也覆盖直接写入 Header map 的非规范大小写键.
func ginCacheHeaderPresent(header http.Header, name string) bool {
	for current := range header {
		if strings.EqualFold(current, name) {
			return true
		}
	}
	return false
}

// ginCacheHasTrailers 检查提交头之后声明的动态 trailer, 不把它们当普通已失效的头修改.
// net/http 允许正文写出后设置 TrailerPrefix, 首次头快照无法表示这部分完整响应.
func ginCacheHasTrailers(header http.Header) bool {
	for name := range header {
		if strings.EqualFold(name, "Trailer") || len(name) >= len(http.TrailerPrefix) &&
			strings.EqualFold(name[:len(http.TrailerPrefix)], http.TrailerPrefix) {
			return true
		}
	}
	return false
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
	store      *httpcache.Cache
	body       bytes.Buffer
	header     http.Header
	capturedAt time.Time
	limit      int
	disabled   bool
}

// storeResponse 只在下游完整成功返回后存储响应, 不把错误、取消或延后声明的 trailer 当完整结果.
// 普通头采用首次提交快照, 避免业务提交后修改的无效头影响真实响应语义.
func (w *cacheResponseWriter) storeResponse(c *gin.Context, key [32]byte) {
	if w.disabled || len(c.Errors) > 0 || c.IsAborted() || c.Request.Context().Err() != nil ||
		ginCacheHasTrailers(w.Header()) {
		return
	}
	header := w.header
	if header == nil {
		header = w.Header()
	}
	if entry, ok := w.store.Prepare(header, w.body.Bytes(), w.Status()); ok {
		entry.StoredAt = w.capturedAt
		w.store.Set(key, entry)
	}
}

// Write 只记录成功发送的字节; 达到预算后释放记录并继续原响应.
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

// WriteHeaderNow 在头真正提交前拍快照, 不缓存业务写完后才修改的头.
func (w *cacheResponseWriter) WriteHeaderNow() {
	w.captureHeader()
	w.ResponseWriter.WriteHeaderNow()
}

// captureHeader 只保存首次发送时的头; 实际可保存的字段由共享内核筛选.
func (w *cacheResponseWriter) captureHeader() {
	if w.header == nil && !w.disabled {
		// 准入检查先于复制, 超大头或私有响应不额外保留完整快照和正文.
		entry, ok := w.store.Prepare(w.Header(), nil, w.Status())
		if !ok {
			w.disabled = true
			return
		}
		w.header = entry.Header
		w.capturedAt = time.Now()
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
func (w *cacheResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
