package httpcache

import (
	"crypto/sha256"
	"encoding/binary"
	"net/http"
	"strings"
)

// Request 是框架无关的请求快照. Host 保留原始请求 Host, EffectiveHost 保存框架根据
// 可信代理规则选择的 Host, 两者都参与键. Header 合并全部值供策略检查, HeaderValues
// 保留原始字段行的数量和顺序供键区分, 防止单行逗号与多行值混用业务可见表示.
type Request struct {
	Method        string
	Scheme        string
	Host          string
	EffectiveHost string
	URI           string
	Header        func(string) string
	HeaderValues  func(string) []string
}

// Key 使用完整 URI, 请求表示和正文计算有边界的 SHA-256, 不保留原始大键.
// 不规范化查询参数顺序或正文语义, 使顺序敏感的应用和不同 Content-Type 保持隔离.
func (c *Cache) Key(req Request, body []byte) [32]byte {
	h := sha256.New()
	var length [8]byte
	writeValue := func(value string) {
		binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
		h.Write(length[:])
		h.Write([]byte(value))
	}
	for _, value := range []string{req.Method, req.Scheme, req.Host, req.EffectiveHost, req.URI} {
		writeValue(value)
	}
	writeHeader := func(name string) {
		values := req.headerValues(name)
		binary.LittleEndian.PutUint64(length[:], uint64(len(values)))
		h.Write(length[:])
		for _, value := range values {
			writeValue(value)
		}
	}
	writeHeader("Content-Type")
	writeHeader("Content-Encoding")
	for _, name := range c.cfg.Vary {
		writeHeader(name)
	}
	binary.LittleEndian.PutUint64(length[:], uint64(len(body)))
	h.Write(length[:])
	h.Write(body)
	// 摘要写入固定栈缓冲区, 避免热命中为临时切片额外分配 32 字节.
	var key [32]byte
	h.Sum(key[:0])
	return key
}

// header 让无请求头的内部调用也可构建键, 不把 nil 回调作为中间件注册错误.
func (r Request) header(name string) string {
	if r.Header == nil {
		return ""
	}
	return r.Header(name)
}

// headerValues 优先保留框架提供的原始字段行, 旧的内部快照没有该回调时按单行处理.
func (r Request) headerValues(name string) []string {
	if r.HeaderValues != nil {
		return r.HeaderValues(name)
	}
	return []string{r.header(name)}
}

// normalizeConfig 补齐固定预算, 规范化并去重 Vary 名称. 内容类型和编码固定进入键,
// 不必再次记录在 Vary 中; 默认维度始终保留, 避免自定义头意外取消内容协商隔离.
func normalizeConfig(cfg Config) Config {
	if cfg.MaxBodyBytes < 0 || cfg.MaxRequestBodyBytes < 0 {
		panic("httpcache: body size limits must not be negative")
	}
	if cfg.Capacity == 0 {
		cfg.Capacity = defaultCapacity
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.MaxRequestBodyBytes == 0 {
		cfg.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	names := append([]string{"Accept", "Accept-Encoding", "Accept-Language", "Origin"}, cfg.Vary...)
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
