package httpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SkipRequest 只允许可共享的普通完整响应参与缓存. 方法和正文上限由适配器检查,
// 认证、会话、重新验证、范围、流式及显式缓存控制请求始终回源.
func SkipRequest(req Request) bool {
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "Upgrade", "Range",
		"If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range",
		"Cache-Control", "Pragma", "Trailer",
	} {
		if req.header(name) != "" {
			return true
		}
	}
	return hasToken(req.header("Connection"), "upgrade") ||
		strings.Contains(strings.ToLower(req.header("Accept")), "text/event-stream")
}

// Prepare 判断完整响应能否共享, 并生成待存储条目. 仅成功的有界正文进入缓存,
// 显式时间头采取拒绝策略, 不在本地重新解释上游 Date/Age/Expires 或延长其新鲜度.
// 返回条目暂时引用输入 Body; Set 会复制, 调用方必须在框架复用缓冲区前完成 Set.
func (c *Cache) Prepare(header http.Header, body []byte, status int) (*Entry, bool) {
	if len(body) > c.cfg.MaxBodyBytes {
		return nil, false
	}
	normalized, ttl, ok := c.responsePolicy(header, status)
	if !ok {
		return nil, false
	}
	stored := make(http.Header, len(normalized))
	for name, values := range normalized {
		if !dynamicResponseHeader(name) {
			stored[name] = append([]string(nil), values...)
		}
	}
	return &Entry{Header: stored, Body: body, TTL: ttl}, true
}

// CanServe 在查询缓存前检查外层已经设置的响应策略, 与 Prepare 共用准入规则.
// 不创建条目或回放头快照, 避免每次热命中为尚不存在的响应重复分配.
func (c *Cache) CanServe(header http.Header, status int) bool {
	_, _, ok := c.responsePolicy(header, status)
	return ok
}

// responsePolicy 只校验并返回只读头视图. 框架通常已规范化字段名, 仅在遇到非规范
// 大小写时复制合并, 确保小写或重复键不能绕过多行隐私和新鲜度检查.
func (c *Cache) responsePolicy(header http.Header, status int) (http.Header, time.Duration, bool) {
	if status != http.StatusOK || !boundedHeaders(header) {
		return nil, 0, false
	}
	normalized := header
	for name := range header {
		if name != http.CanonicalHeaderKey(name) {
			normalized = make(http.Header, len(header))
			for original, values := range header {
				canonical := http.CanonicalHeaderKey(original)
				normalized[canonical] = append(normalized[canonical], values...)
			}
			break
		}
	}
	if unsafeResponse(normalized) || !c.supportsVary(strings.Join(normalized.Values("Vary"), ",")) {
		return nil, 0, false
	}
	ttl, ok := responseTTL(strings.Join(normalized.Values("Cache-Control"), ","), c.ttl)
	return normalized, ttl, ok
}

// dynamicResponseHeader 排除已知逐请求字段及框架根据实际正文生成的长度.
// 其他端到端字段完整保留, 避免静默丢失安全策略、分页和应用自定义元数据.
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

// IsRepresentationHeader 标记必须与缓存正文一致的字段, 这些字段覆盖框架的初始默认值.
// 其他端到端字段回放时保留外层当前请求的同名值, 允许安全头中间件持续更新策略.
func IsRepresentationHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Content-Type", "Content-Encoding", "Content-Language", "Content-Disposition", "Content-Length",
		"Cache-Control", "Vary", "Etag", "Last-Modified", "Content-Location", "Digest", "Content-Digest", "Repr-Digest", "Age":
		return true
	}
	return false
}

// unsafeResponse 拒绝会话、认证、局部正文和连接级状态, 不重放每次请求生成的头.
// 即使危险字段值为空也跳过, 保留处理器明确设置该字段的意图.
func unsafeResponse(header http.Header) bool {
	for name := range header {
		if len(name) >= len(http.TrailerPrefix) && strings.EqualFold(name[:len(http.TrailerPrefix)], http.TrailerPrefix) {
			return true
		}
	}
	for _, name := range []string{
		"Set-Cookie", "Set-Cookie2", "Www-Authenticate", "Content-Range",
		"Connection", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade", "Date", "Age", "Expires", "Pragma",
	} {
		if _, exists := header[name]; exists {
			return true
		}
	}
	return strings.Contains(strings.ToLower(strings.Join(header.Values("Content-Type"), ",")), "text/event-stream")
}

// supportsVary 要求响应声明的全部变化维度都已进入请求键, 未知维度或星号直接回源.
// 内容类型和编码是固定维度, 即使没有出现在用户配置中也始终安全.
func (c *Cache) supportsVary(value string) bool {
	if value == "" {
		return true
	}
	for _, name := range strings.Split(value, ",") {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		if name == "Content-Type" || name == "Content-Encoding" {
			continue
		}
		if name == "*" || !validHeaderName(name) {
			return false
		}
		found := false
		for _, configured := range c.cfg.Vary {
			if name == configured {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// responseTTL 仅接受有限的正 max-age/s-maxage, 两者同时存在时取更短值.
// 私有、禁用缓存、重复或无效的新鲜度声明均拒绝, 不把零 TTL 传给永久存储语义.
// ReplayHeader 在命中时补充 Age, 避免原 Cache-Control 让下游缓存重新计时.
func responseTTL(value string, ttl time.Duration) (time.Duration, bool) {
	if value == "" {
		return ttl, true
	}
	seen := make(map[string]bool, 2)
	for _, part := range strings.Split(value, ",") {
		name, argument, hasValue := strings.Cut(strings.TrimSpace(part), "=")
		name = strings.ToLower(strings.TrimSpace(name))
		switch name {
		case "no-store", "no-cache", "private":
			return 0, false
		case "max-age", "s-maxage":
			if !hasValue || seen[name] {
				return 0, false
			}
			seen[name] = true
			seconds, ok := deltaSeconds(argument)
			if !ok {
				return 0, false
			}
			ttl = min(ttl, time.Duration(seconds)*time.Second)
		}
	}
	return ttl, true
}

// deltaSeconds 只接受完整的非零十进制秒数和成对引号, 不容忍负值、符号、缺失
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

// hasToken 按逗号分隔匹配 Connection 等列表值, 不将子串误认成协议控制 token.
func hasToken(value, token string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}
