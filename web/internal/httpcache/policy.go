package httpcache

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// connectionHeader 与 acceptHeader 按列表语义检查: 前者的 upgrade 表示协议升级, 后者的事件流表示长连接推送.
// eventStream 是 SSE 媒体类型, 请求 Accept 与响应 Content-Type 都按它跳过缓存.
const (
	connectionHeader = "Connection"
	acceptHeader     = "Accept"
	eventStream      = "text/event-stream"
)

// skipRequestHeaders 是出现非空值即跳过缓存的请求头: 凭据, 会话, 协议升级, 范围, 条件请求与客户端缓存控制.
// listRequestHeaders 按列表语义检查, keyHeaders 固定进入请求键.
// 三者加上 Vary 构成内核读取的全部请求头, 也是适配器需要收集的范围 (Cache.RequestHeaderName), 单一来源防止漂移.
var (
	skipRequestHeaders = [...]string{
		"Authorization", "Proxy-Authorization", "Cookie", "Upgrade", "Range",
		"If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range",
		"Cache-Control", "Pragma", "Trailer",
	}
	listRequestHeaders = [...]string{connectionHeader, acceptHeader}
	keyHeaders         = [...]string{"Content-Type", "Content-Encoding"}
)

// SkipRequest 只允许可共享的普通完整响应参与缓存. 方法和正文上限由适配器检查,
// 认证, 会话, 重新验证, 范围, 流式及显式缓存控制请求始终回源.
func SkipRequest(req Request) bool {
	for _, name := range skipRequestHeaders {
		for _, value := range req.Header[name] {
			if value != "" {
				return true
			}
		}
	}
	return hasToken(req.Header[connectionHeader], "upgrade") || hasEventStream(req.Header[acceptHeader])
}

// Prepare 判断完整响应能否共享, 并生成待存储条目. 仅成功的有界正文进入缓存,
// 显式时间头采取拒绝策略, 不在本地重新解释上游 Date/Age/Expires 或延长其新鲜度.
// 返回条目独占 Header map 和值切片, Body 暂时引用输入; 调用方须在 Set 前完成头筛选.
// Set 共享并冻结 Header, 只复制 Body, 必须在框架复用正文缓冲区前完成发布.
func (c *Cache) Prepare(header http.Header, body []byte, status int) (*Entry, bool) {
	if len(body) > c.cfg.MaxBodyBytes || !boundedHeaders(header) {
		return nil, false
	}
	ttl, ok := c.responsePolicy(header, status)
	if !ok {
		return nil, false
	}
	stored := make(http.Header, len(header))
	for name, values := range header {
		if !dynamicResponseHeader(name) {
			stored[name] = append([]string(nil), values...)
		}
	}
	return &Entry{Header: stored, Body: body, TTL: ttl}, true
}

// CanServe 在查询前检查外层响应策略, 不复制头或重复检查仅存储时需要的体积预算.
func (c *Cache) CanServe(header http.Header, status int) bool {
	_, ok := c.responsePolicy(header, status)
	return ok
}

// responsePolicy 按规范名检查头, 含非规范字段名的响应由 unsafeResponse 一律拒绝.
// 多行缓存控制和 Vary 逐行逐项检查, 不允许后续字段行被首值遮蔽.
func (c *Cache) responsePolicy(header http.Header, status int) (time.Duration, bool) {
	if status != http.StatusOK || unsafeResponse(header) || !c.supportsVary(header["Vary"]) {
		return 0, false
	}
	return responseTTL(header["Cache-Control"], c.ttl)
}

// dynamicResponseHeader 排除已知逐请求字段及框架根据实际正文生成的长度.
// 其他端到端字段完整保留, 避免静默丢失安全策略, 分页和应用自定义元数据.
func dynamicResponseHeader(name string) bool {
	if strings.HasPrefix(name, "Access-Control-") || name == "Ratelimit" ||
		strings.HasPrefix(name, "Ratelimit-") || strings.HasPrefix(name, "X-Ratelimit-") ||
		strings.HasPrefix(name, "X-Rate-Limit-") {
		return true
	}
	switch name {
	case "Content-Length", "X-Request-Id", "Request-Id", "X-Correlation-Id", "Correlation-Id",
		"Traceparent", "Tracestate", "Baggage", "Server-Timing", "X-Response-Time", "X-Runtime":
		return true
	}
	return false
}

// IsRepresentationHeader 接收规范头名, 标记必须与缓存正文一致的字段.
// 其他端到端字段回放时保留外层当前请求的同名值, 允许安全头中间件持续更新策略.
func IsRepresentationHeader(name string) bool {
	switch name {
	case "Content-Type", "Content-Encoding", "Content-Language", "Content-Disposition", "Content-Length",
		"Cache-Control", "Vary", "Etag", "Last-Modified", "Content-Location", "Digest", "Content-Digest", "Repr-Digest", "Age":
		return true
	}
	return false
}

// unsafeResponse 拒绝会话, 认证, 局部正文和连接级状态, 不重放每次请求生成的头.
// 即使危险字段值为空也跳过, 保留处理器明确设置该字段的意图. 后续检查按规范名查找, 直接写入 map 的
// "set-cookie" 或 "cache-control: private" 会被客户端照常处理却逃过检查, 因此含非规范名即失败拒绝.
// 危险名单, 规范名与 trailer 前缀检查共用一次遍历: switch 按长度和内容二分比较, 不为每个名单项哈希查找;
// 已规范的名称由 CanonicalHeaderKey 原样返回且不分配.
func unsafeResponse(header http.Header) bool {
	for name := range header {
		switch name {
		case "Set-Cookie", "Set-Cookie2", "Www-Authenticate", "Content-Range",
			"Connection", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization",
			"Te", "Trailer", "Transfer-Encoding", "Upgrade", "Date", "Age", "Expires", "Pragma":
			return true
		}
		if name != http.CanonicalHeaderKey(name) ||
			len(name) >= len(http.TrailerPrefix) && strings.EqualFold(name[:len(http.TrailerPrefix)], http.TrailerPrefix) {
			return true
		}
	}
	return hasEventStream(header["Content-Type"])
}

// supportsVary 要求响应声明的全部变化维度都已进入请求键, 未知维度, 行内空项或星号直接回源.
// 内容类型和编码是固定维度, 即使没有出现在用户配置中也始终安全. 逐行用 Cut 迭代, 不分配.
func (c *Cache) supportsVary(values []string) bool {
	for _, value := range values {
		// 整行为空或只有空白等同未声明; 非空行内的空项仍按非法名拒绝, 保持保守准入.
		for rest, more := value, strings.TrimSpace(value) != ""; more; {
			var part string
			part, rest, more = strings.Cut(rest, ",")
			name := http.CanonicalHeaderKey(strings.TrimSpace(part))
			if name == "Content-Type" || name == "Content-Encoding" {
				continue
			}
			if name == "*" || !validHeaderName(name) || !slices.Contains(c.cfg.Vary, name) {
				return false
			}
		}
	}
	return true
}

// responseTTL 仅接受有限的正 max-age/s-maxage, 两者同时存在时取更短值.
// 私有, 禁用缓存, 重复或无效的新鲜度声明均拒绝, 不把零 TTL 传给永久存储语义.
// 适配器回放时补充 Age (Entry.Age), 避免原 Cache-Control 让下游缓存重新计时.
func responseTTL(values []string, ttl time.Duration) (time.Duration, bool) {
	var maxAge, sMaxAge bool
	for _, value := range values {
		for rest, more := value, true; more; {
			var part string
			part, rest, more = strings.Cut(rest, ",")
			name, argument, hasValue := strings.Cut(strings.TrimSpace(part), "=")
			// 指令名大小写无关, 用 EqualFold 比较而非转小写, 大写写法也不分配.
			name = strings.TrimSpace(name)
			seen := &maxAge
			switch {
			case strings.EqualFold(name, "no-store"), strings.EqualFold(name, "no-cache"), strings.EqualFold(name, "private"):
				return 0, false
			case strings.EqualFold(name, "s-maxage"):
				seen = &sMaxAge
			case strings.EqualFold(name, "max-age"):
			default:
				continue
			}
			if !hasValue || *seen {
				return 0, false
			}
			*seen = true
			seconds, ok := deltaSeconds(argument)
			if !ok {
				return 0, false
			}
			ttl = min(ttl, time.Duration(seconds)*time.Second)
		}
	}
	return ttl, true
}

// deltaSeconds 只接受完整的非零十进制秒数和成对引号, 不容忍负值, 符号, 缺失
// 引号或溢出. 32 位上限足以覆盖实用 TTL, 同时避免转为 Duration 后乘法溢出.
func deltaSeconds(value string) (uint64, bool) {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	if value == "" {
		return 0, false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(value, 10, 32)
	return seconds, err == nil && seconds > 0
}

// hasToken 按逗号分隔逐行匹配 Connection 等列表值, 不将子串误认成协议控制 token. 用 Cut 迭代, 不分配.
func hasToken(values []string, token string) bool {
	for _, value := range values {
		for rest, more := value, true; more; {
			var part string
			part, rest, more = strings.Cut(rest, ",")
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// hasEventStream 判断逗号分隔的媒体类型列表中是否有 text/event-stream, 忽略参数与 ASCII 大小写.
// 逐项比较类型本身而非子串; 该类型必含 '-', 先以向量化字节查找跳过常见的浏览器 Accept,
// 只有可能匹配的字段行才逐项解析, 不转小写也不分配. 单值的 Content-Type 同样适用.
func hasEventStream(values []string) bool {
	for _, value := range values {
		if strings.IndexByte(value, '-') < 0 {
			continue
		}
		for rest, more := value, true; more; {
			var part string
			part, rest, more = strings.Cut(rest, ",")
			part, _, _ = strings.Cut(part, ";")
			if strings.EqualFold(strings.TrimSpace(part), eventStream) {
				return true
			}
		}
	}
	return false
}
