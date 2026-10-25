// Package httpcache 提供 Gin/Fiber 共用的进程内响应缓存策略, 不负责协议框架的响应捕获.
package httpcache

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/fufuok/freelru"
)

const (
	defaultCapacity            = 256
	defaultMaxBodyBytes        = 256 << 10
	defaultMaxRequestBodyBytes = 64 << 10
	defaultTTL                 = 10 * time.Minute
	maxHeaderBytes             = 64 << 10
)

// Config 限制单个中间件实例的内存占用和可区分的请求表示.
// 零值使用默认值, Vary 在默认四个请求头之后追加, 不覆盖安全默认维度.
type Config struct {
	Capacity            uint32
	MaxBodyBytes        int
	MaxRequestBodyBytes int
	Vary                []string
}

// Entry 只保存已准入的 200 响应. Header 和 Body 在 Set 后不可修改,
// 框架适配器应复制数据写出, 不能让可复用的请求缓冲区引用缓存内存.
type Entry struct {
	Header http.Header
	Body   []byte
	TTL    time.Duration
	// StoredAt 是响应年龄起点, 流式写出适配器可在首次提交头时设置.
	// 零值由 Set 补齐; 已消耗的生成时间会从存储 TTL 中扣除.
	StoredAt time.Time
	// expiresAt 由 Set 保留单调时钟期限, 避免底层墙钟回拨延长 HTTP 新鲜度.
	expiresAt time.Time
}

// Cache 使用固定条目数的 LRU 和惰性过期, 不启动清理 goroutine.
// Config 是规范化后的只读配置快照, 不支持运行期修改; 内部保留独立副本防止切片别名.
type Cache struct {
	Config Config

	cfg   Config
	ttl   time.Duration
	store *freelru.SyncedLRU[[32]byte, *Entry]
	mu    sync.Mutex
	locks map[[32]byte]*keyLock
}

// keyLock 的引用计数覆盖持有者和等待者, 最后一个请求离开时即可删除登记,
// 不因历史请求键持续增长, 也不使不同请求键互相等待业务处理.
type keyLock struct {
	mu   sync.Mutex
	refs int
}

// New 创建单个路由或路由组独享的缓存. 非正 TTL 使用十分钟, 负数大小或非法
// Vary 名称会在注册中间件时 panic, 避免启动后静默变成无界缓存或混淆请求表示.
func New(ttl time.Duration, cfg Config) *Cache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	cfg = normalizeConfig(cfg)
	store, err := freelru.NewSyncedDefault[[32]byte, *Entry](cfg.Capacity, ttl)
	if err != nil {
		panic(fmt.Errorf("httpcache: create response cache: %w", err))
	}
	publicCfg := cfg
	publicCfg.Vary = append([]string(nil), cfg.Vary...)
	return &Cache{
		Config: publicCfg,
		cfg:    cfg,
		ttl:    ttl,
		store:  store,
		locks:  make(map[[32]byte]*keyLock),
	}
}

// Get 返回仍新鲜的只读条目并更新 LRU 次序, 命中不会续期.
// 返回值由全部命中请求共享, 调用方不能修改其 Header 或 Body.
func (c *Cache) Get(key [32]byte) (*Entry, bool) {
	entry, ok := c.store.Get(key)
	if !ok || !time.Now().Before(entry.expiresAt) {
		// 不按键删除, 防止检查旧快照期间并发 Set 已经替换成新条目.
		return nil, false
	}
	return entry, true
}

// Set 深复制条目隔离框架的缓冲区复用, 只允许存储 Prepare 返回的正 TTL 条目.
// 再次限制 TTL 与体积, 防止内部适配器意外绕过准入时产生永久或超大条目.
func (c *Cache) Set(key [32]byte, entry *Entry) {
	if entry == nil || entry.TTL <= 0 || len(entry.Body) > c.cfg.MaxBodyBytes || !boundedHeaders(entry.Header) {
		return
	}
	ttl := min(entry.TTL, c.ttl)
	now := time.Now()
	storedAt := entry.StoredAt
	if storedAt.IsZero() {
		storedAt = now
	} else {
		ttl -= max(now.Sub(storedAt), 0)
	}
	if ttl <= 0 {
		return
	}
	c.store.AddWithLifetime(key, &Entry{
		Header:    entry.Header.Clone(),
		Body:      bytes.Clone(entry.Body),
		TTL:       ttl,
		StoredAt:  storedAt,
		expiresAt: now.Add(ttl),
	}, ttl)
}

// ReplayHeader 返回独立的头副本并补充缓存年龄, 使下游按原 Cache-Control 扣除
// 已经过的本地时间. 秒数向上取整以保守缩短新鲜度, 不将亚秒年龄舍掉后重新计时.
func (e *Entry) ReplayHeader() http.Header {
	header := e.Header.Clone()
	if header == nil {
		header = make(http.Header)
	}
	elapsed := max(time.Since(e.StoredAt), 0)
	seconds := elapsed / time.Second
	if elapsed%time.Second != 0 {
		seconds++
	}
	header.Set("Age", strconv.FormatInt(int64(seconds), 10))
	return header
}

// Acquire 串行化同一请求键的回源. 调用方必须 defer 返回的释放函数, 并在取得
// 锁后再次 Get. 只有已准入的缓存可供后续请求复用, 不共享失败或私人响应结果.
// 释放函数只能执行一次; 不提供取消等待, 由各框架按自身上下文管理处理生命周期.
func (c *Cache) Acquire(key [32]byte) func() {
	c.mu.Lock()
	lock := c.locks[key]
	if lock == nil {
		lock = &keyLock{}
		c.locks[key] = lock
	}
	lock.refs++
	c.mu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		c.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(c.locks, key)
		}
		c.mu.Unlock()
	}
}

// boundedHeaders 在复制前限制整个原始头集合, 动态头即使不进入缓存也受预算限制.
// 逐次从剩余额度扣减可避免巨量头字段长度相加溢出.
func boundedHeaders(header http.Header) bool {
	remaining := maxHeaderBytes
	for name, values := range header {
		if len(name) > remaining {
			return false
		}
		remaining -= len(name)
		for _, value := range values {
			if remaining < 4 || len(value) > remaining-4 {
				return false
			}
			remaining -= len(value) + 4
		}
	}
	return true
}
