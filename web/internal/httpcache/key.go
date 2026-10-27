package httpcache

import (
	"crypto/sha256"
	"encoding/binary"
	"net/http"
	"strings"

	"github.com/fufuok/pkg/utils"
)

// Request 是框架无关的请求快照. Host 保留原始请求 Host, EffectiveHost 保存框架根据
// 可信代理规则选择的 Host, 两者都参与键; EffectiveScheme 同理保存代理中间件改写后的协议 (未改写为空),
// 与连接协议 Scheme 分别进入键, 不在命中路径拼接字符串. URI 是原始请求目标, Path 与 Query 是框架路由和业务实际读取的
// 路径与查询串: 外层按未进入键的输入 (如 User-Agent) 改写后两者与 URI 不同, 一并进入键才能隔离改写结果;
// 未改写时它们由 URI 唯一确定, 不额外拆分缓存. Header 保留规范字段名及原始字段行顺序,
// 防止单行逗号与多行值混用业务可见表示; 只在业务执行前同步读取, 不保留到存储条目.
type Request struct {
	Method          string
	Scheme          string
	EffectiveScheme string
	Host            string
	EffectiveHost   string
	URI             string
	Path            string
	Query           string
	Header          http.Header
}

// Key 使用完整 URI, 请求表示和正文计算有边界的 SHA-256, 不保留原始大键.
// 不规范化查询参数顺序或正文语义, 使顺序敏感的应用和不同 Content-Type 保持隔离.
// 头按规范名直接索引 (名称均为常量或构造时规范化的 Vary), 整个过程不分配.
func (c *Cache) Key(req Request, body []byte) [32]byte {
	h := sha256.New()
	// 键材料按 "uvarint 长度 + 字节" 编码, 前缀无歧义; 先拼入栈缓冲区再成批写入摘要, 减少逐字段调用.
	// 剩余空间不足时先冲刷, 超过整个缓冲区的字段直接写入, 长头值也不分配.
	var buf [512]byte
	b := buf[:0]
	flush := func(need int) {
		if len(b)+need > len(buf) {
			h.Write(b)
			b = buf[:0]
		}
	}
	writeLength := func(n int) {
		flush(binary.MaxVarintLen64)
		b = binary.AppendUvarint(b, uint64(n))
	}
	writeValue := func(value string) {
		writeLength(len(value))
		if flush(len(value)); len(value) > len(buf) {
			h.Write(utils.S2B(value))
			return
		}
		b = append(b, value...)
	}
	for _, value := range []string{
		req.Method, req.Scheme, req.EffectiveScheme, req.Host, req.EffectiveHost, req.URI, req.Path, req.Query,
	} {
		writeValue(value)
	}
	writeHeader := func(name string) {
		values := req.Header[name]
		writeLength(len(values))
		for _, value := range values {
			writeValue(value)
		}
	}
	for _, name := range keyHeaders {
		writeHeader(name)
	}
	for _, name := range c.cfg.Vary {
		writeHeader(name)
	}
	writeLength(len(body))
	h.Write(b)
	h.Write(body)
	// 摘要写入固定栈缓冲区, 避免热命中为临时切片额外分配 32 字节.
	var key [32]byte
	h.Sum(key[:0])
	return key
}

// normalizeConfig 补齐固定预算, 规范化并去重 Vary 名称. 内容类型和编码固定进入键,
// 不必再次记录在 Vary 中; 默认维度始终保留, 避免自定义头意外取消内容协商隔离.
// 转发头默认进入键: 应用按其生成绝对地址时, 不分区会让单个伪造请求污染共享缓存.
func normalizeConfig(cfg Config) Config {
	if cfg.MaxBodyBytes < 0 || cfg.MaxRequestBodyBytes < 0 {
		panic("httpcache: body size limits must not be negative")
	}
	if cfg.Capacity == 0 {
		cfg.Capacity = defaultCapacity
	}
	// freelru 会按 Capacity 立即分配 bucket 与 element 数组, 在构造前拒绝误配.
	if cfg.Capacity > maxCapacity {
		panic("httpcache: capacity exceeds 65536 entries")
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.MaxRequestBodyBytes == 0 {
		cfg.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	names := append([]string{
		"Accept", "Accept-Encoding", "Accept-Language", "Origin", "Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto",
	}, cfg.Vary...)
	cfg.Vary = make([]string, 0, len(names))
	seen := map[string]bool{"Content-Type": true, "Content-Encoding": true}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "*" || !validHeaderName(name) {
			panic("httpcache: Vary must contain valid request header names")
		}
		name = http.CanonicalHeaderKey(name)
		if !seen[name] {
			cfg.Vary = append(cfg.Vary, name)
			seen[name] = true
		}
	}
	return cfg
}

// validHeaderName 按 HTTP token 语法检查名称, 防止空名或分隔符变成不可匹配的 Vary.
func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		ch := name[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(ch)) {
			return false
		}
	}
	return true
}
