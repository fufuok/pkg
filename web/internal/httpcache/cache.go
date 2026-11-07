// Package httpcache 提供 Gin/Fiber 共用的进程内响应缓存策略, 不负责协议框架的响应捕获.
package httpcache

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fufuok/freelru"
	"github.com/fufuok/pkg/utils"
)

const (
	defaultCapacity            = 256
	maxCapacity                = 1 << 16
	defaultMaxBodyBytes        = 256 << 10
	defaultMaxRequestBodyBytes = 64 << 10
	defaultTTL                 = 10 * time.Minute
	maxHeaderBytes             = 64 << 10
	// passWindow 是未准入响应之后同键请求跳过回源合并 (hit-for-pass) 的最长时间, 实际取 min(ttl, passWindow).
	// 与 nginx proxy_cache_lock_timeout 默认值同量级: 足以覆盖常见业务耗时, 避免慢的不可缓存接口
	// 频繁回到串行排队. 窗口结束后恢复合并, 但已经放行的请求仍可能并发完成.
	passWindow = 5 * time.Second
	// maxPassKeys 限制直通标记表的固定容量. 标记只对数秒内有并发的键有意义, 超出后按 LRU 淘汰,
	// 被淘汰的键仅退化为等待领头请求结束后再并发回源, 不随 Capacity 放大预分配内存.
	maxPassKeys = 1024
)

const (
	ruleNone requestHeaderRule = iota
	ruleSkipNonEmpty
	ruleConnection
	ruleAccept
)

type requestHeaderRule uint8

type requestHeaderInfo struct {
	canonical string
	rule      requestHeaderRule
	inKey     bool
}

// Config 限制单个中间件实例的内存占用和可区分的请求表示.
// 零值使用默认值, Vary 在默认请求头之后追加, 不覆盖安全默认维度.
// 构造后不应修改配置或其中的 Vary 切片, 缓存内部使用的是规范化后的独立副本.
type Config struct {
	// Capacity 是本实例最多保存的响应条目数, 满后按 LRU 淘汰; 零值使用 256,
	// 大于 65536 在构造时 panic, 避免 freelru 预分配巨量空表.
	Capacity uint32
	// MaxBodyBytes 是单个响应正文可缓存的最大字节数, 超限响应照常发送但不缓存;
	// 零值使用 256KiB, 负数在构造时 panic.
	MaxBodyBytes int
	// MaxRequestBodyBytes 是 CacheByRawData 参与请求键的原始正文上限, 超限请求透传给业务且不缓存;
	// 零值使用 64KiB, 负数在构造时 panic. CacheByRequestURI 只接受无正文 GET, 不使用该值.
	MaxRequestBodyBytes int
	// Vary 是追加进请求键的请求头名称, 在默认的 Accept, Accept-Encoding, Accept-Language, Origin,
	// Forwarded, X-Forwarded-Host, X-Forwarded-Proto 之后规范化并去重. 响应声明的 Vary 维度
	// 必须全部在此列出才会缓存; 空名, 非法名称或 "*" 在构造时 panic.
	Vary []string
}

// Entry 只保存已准入的 200 响应. Header 由 Prepare 生成并归调用方独占, 首次 Set 后即冻结, 此后不能再
// OmitInherited 或修改其值切片; Body 在 Set 时紧凑复制. 框架适配器应复制数据写出,
// 不能让可复用的请求缓冲区引用缓存内存.
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
	// pass 是直通窗口长度; passes 保存各键直通截止的单调时钟时刻, 固定容量且惰性过期.
	pass   time.Duration
	passes *freelru.SyncedLRU[[32]byte, time.Time]
	mu     sync.Mutex
	locks  map[[32]byte]*keyLock
	// requestHeaders 以小写名映射到规范名、跳过规则及是否进入请求键; maxRequestName 是其中最长的名称.
	requestHeaders map[string]requestHeaderInfo
	maxRequestName int
}

// keyLock 用容量为 1 的令牌通道串行化同键回源, 使等待者可响应请求 Context 取消/到期.
// 引用计数覆盖当前持有者与仍在排队的等待者, 最后一个请求离开时删除登记, 不因历史请求键持续增长.
type keyLock struct {
	ch   chan struct{}
	refs int
	// owner 是当前持锁请求的身份, 由 Cache.mu 保护; 同一请求重入时据此识别, 不在自己持有的锁上等待.
	owner any
}

// New 创建单个路由或路由组独享的缓存. 非正 TTL 使用十分钟, 容量超过上限,
// 负数大小或非法 Vary 名称会在注册中间件时 panic, 避免巨量空表或混淆请求表示.
func New(ttl time.Duration, cfg Config) *Cache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	cfg = normalizeConfig(cfg)
	store, err := freelru.NewSyncedDefault[[32]byte, *Entry](cfg.Capacity, ttl)
	if err != nil {
		panic(fmt.Errorf("httpcache: create response cache: %w", err))
	}
	// 直通窗口不超过缓存 TTL, 避免极短 TTL 的路由长时间失去回源合并.
	pass := min(ttl, passWindow)
	passes, err := freelru.NewSyncedDefault[[32]byte, time.Time](min(cfg.Capacity, maxPassKeys), pass)
	if err != nil {
		panic(fmt.Errorf("httpcache: create pass marker cache: %w", err))
	}
	publicCfg := cfg
	publicCfg.Vary = append([]string(nil), cfg.Vary...)
	c := &Cache{
		Config:         publicCfg,
		cfg:            cfg,
		ttl:            ttl,
		store:          store,
		pass:           pass,
		passes:         passes,
		locks:          make(map[[32]byte]*keyLock),
		requestHeaders: make(map[string]requestHeaderInfo),
	}
	registerHeader := func(name string, rule requestHeaderRule, inKey bool) {
		lower := strings.ToLower(name)
		info := c.requestHeaders[lower]
		info.canonical = name
		if rule != ruleNone {
			info.rule = rule
		}
		if inKey {
			info.inKey = true
		}
		c.requestHeaders[lower] = info
		c.maxRequestName = max(c.maxRequestName, len(name))
	}
	for _, name := range keyHeaders {
		registerHeader(name, ruleNone, true)
	}
	for _, name := range cfg.Vary {
		registerHeader(name, ruleNone, true)
	}
	for _, name := range skipRequestHeaders {
		registerHeader(name, ruleSkipNonEmpty, false)
	}
	registerHeader(connectionHeader, ruleConnection, false)
	registerHeader(acceptHeader, ruleAccept, false)
	return c
}

// lookupRequestHeader 按 ASCII 大小写无关名称查找已登记的请求头规则, 常规长度在栈上转小写不分配.
func (c *Cache) lookupRequestHeader(name []byte) (requestHeaderInfo, bool) {
	if len(name) == 0 || len(name) > c.maxRequestName {
		return requestHeaderInfo{}, false
	}
	var buf [64]byte
	lower := buf[:0]
	if len(name) > len(buf) {
		lower = make([]byte, 0, len(name))
	}
	for _, ch := range name {
		if 'A' <= ch && ch <= 'Z' {
			ch += 'a' - 'A'
		}
		lower = append(lower, ch)
	}
	info, ok := c.requestHeaders[string(lower)]
	return info, ok
}

// RequestHeaderName 判断请求头是否参与跳过检查或请求键, 是则返回规范名. 名称按 ASCII 大小写无关匹配,
// 关闭头名规范化的框架同样能识别小写凭据. 在栈上转小写后查表, 常规长度不分配; 适配器据此只收集需要的字段.
func (c *Cache) RequestHeaderName(name []byte) (string, bool) {
	info, ok := c.lookupRequestHeader(name)
	return info.canonical, ok
}

// InspectRequestHeader 零分配检查单个原始请求头字段行: 若触发跳过规则则 skip=true,
// 否则返回规范名及该字段是否参与请求键计算 (inKey), 供适配器在不走缓存的请求上跳过 map 分配.
func (c *Cache) InspectRequestHeader(name, value []byte) (canonical string, inKey, skip bool) {
	info, ok := c.lookupRequestHeader(name)
	if !ok {
		return "", false, false
	}
	switch info.rule {
	case ruleNone:
	case ruleSkipNonEmpty:
		if len(value) > 0 {
			return "", false, true
		}
	case ruleConnection:
		if hasTokenValue(utils.B2S(value), "upgrade") {
			return "", false, true
		}
	case ruleAccept:
		if hasEventStreamValue(utils.B2S(value)) {
			return "", false, true
		}
	}
	return info.canonical, info.inKey, false
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

// Set 发布已通过 Prepare 的条目: 创建独立的存储条目并紧凑复制正文, 隔离框架缓冲区复用;
// Header 已由 Prepare 与原始响应头隔离, 直接共享而不再复制, 同一冻结 Header 可发布到多个键.
// 调用方须保证正文未超预算; 体积和头策略不重复检查, 这里只扣除生成时间, 避免过期响应重新获得完整 TTL.
func (c *Cache) Set(key [32]byte, entry *Entry) {
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
		Header:    entry.Header,
		Body:      bytes.Clone(entry.Body),
		TTL:       ttl,
		StoredAt:  storedAt,
		expiresAt: now.Add(ttl),
	}, ttl)
	// 直通窗口内的并发回源一旦得到可准入响应即结束窗口, 条目到期后的下一轮 miss 恢复合并.
	c.passes.Remove(key)
}

// OmitInherited 删除与外层在业务执行前已设置值完全相同的非表示头, 只保留下游业务设置或改写的字段.
// 外层中间件在命中时仍会为当前请求重新设置这些头; 存入条目只会把按访客条件 (未进入键的输入) 设置的值
// 回放给其他访客. 表示头描述缓存正文, 即使来自外层也必须保留. 业务恰好设置了与外层相同的值时按外层处理.
// 此方法会修改 Header, 只能在首次 Set 前调用.
func (e *Entry) OmitInherited(outer http.Header) {
	for name, values := range outer {
		if !IsRepresentationHeader(name) && slices.Equal(e.Header[name], values) {
			delete(e.Header, name)
		}
	}
}

// Age 返回向上取整的秒龄, 适配器回放时作为 Age 头, 使下游按原 Cache-Control 扣除已经过的本地时间.
// 秒数向上取整以保守缩短新鲜度, 不将亚秒年龄舍掉后重新计时.
func (e *Entry) Age() string {
	elapsed := max(time.Since(e.StoredAt), 0)
	seconds := elapsed / time.Second
	if elapsed%time.Second != 0 {
		seconds++
	}
	return strconv.FormatInt(int64(seconds), 10)
}

// Acquire 合并同一请求键的回源, 等价于 AcquireContext(context.Background(), key, owner).
func (c *Cache) Acquire(key [32]byte, owner any) func(abandoned bool) {
	return c.AcquireContext(context.Background(), key, owner)
}

// AcquireContext 在请求 ctx 约束下合并同一请求键的回源.
// 调用方必须 defer 返回的释放函数, 并在返回后再次检查 ctx 与 Get; 只有已准入的缓存可供后续请求复用.
//
// 持锁者释放时若该键仍无新鲜条目 (未准入, 超限, 非 200, 错误, panic 或已经到期等),
// 释放前先登记 min(ttl, passWindow) 的直通窗口 (hit-for-pass): 已排队的等待者取得令牌后立即放手
// 并发回源, 窗口内新到达的请求也不再排队. 直通请求不持有键锁, 其可准入响应照常写入并结束窗口;
// 窗口到期后恢复单个领头请求回源.
// 释放时传入 abandoned=true 表示持锁请求未执行业务或因客户端取消而没有得到结果, 不代表响应不可缓存:
// 不登记直通, 由下一位等待者接任领头, 避免单个断开或超时的客户端让全部排队请求同时回源.
//
// 等待者在排队期间若遇到 ctx 结束 (客户端断开或外层请求期限到达), 立即扣减等待计数并返回无操作释放函数,
// 避免在慢领头请求上堆积已取消的等待协程; 不设置固定等锁超时, 确保慢接口在微缓存场景下仍能合并回源而不发生同步惊群.
//
// owner 标识当前请求 (框架上下文指针, 须可比较), 非 nil 时同一请求经内部重路由或重复注册再次进入时
// 直接直通: 持锁者就在其调用栈上, 等待必然自锁. nil 不参与识别. 释放函数只能执行一次.
func (c *Cache) AcquireContext(ctx context.Context, key [32]byte, owner any) func(abandoned bool) {
	if c.passing(key) {
		return releasePass
	}
	c.mu.Lock()
	lock := c.locks[key]
	if lock == nil {
		// 首个领头请求直接持有令牌 (通道保持为空), 无需再次加锁写 owner.
		lock = &keyLock{
			ch:    make(chan struct{}, 1),
			refs:  1,
			owner: owner,
		}
		c.locks[key] = lock
		c.mu.Unlock()
		return c.makeRelease(key, lock)
	}
	if owner != nil && lock.owner == owner {
		c.mu.Unlock()
		return releasePass
	}
	lock.refs++
	c.mu.Unlock()

	var done <-chan struct{}
	if ctx != nil {
		if ctx.Err() != nil {
			c.leaveWaiter(key, lock)
			return releasePass
		}
		done = ctx.Done()
	}
	if done == nil {
		<-lock.ch
	} else {
		select {
		case <-lock.ch:
		case <-done:
			c.leaveWaiter(key, lock)
			return releasePass
		}
	}

	// 等待期间上一持锁者可能已登记直通, 立即把令牌传给下一位等待者并直通放行.
	if c.passing(key) {
		c.unlock(key, lock)
		return releasePass
	}
	c.mu.Lock()
	lock.owner = owner
	c.mu.Unlock()
	return c.makeRelease(key, lock)
}

// makeRelease 生成当前令牌持有者的唯一释放闭包.
func (c *Cache) makeRelease(key [32]byte, lock *keyLock) func(abandoned bool) {
	return func(abandoned bool) {
		// 先登记再放锁, 保证被唤醒的等待者一定能看到直通标记.
		if !abandoned && !c.fresh(key) {
			c.passes.AddWithLifetime(key, time.Now().Add(c.pass), c.pass)
		}
		c.unlock(key, lock)
	}
}

// leaveWaiter 在等待者因 Context 结束放弃排队时扣减引用计数; 若持有者此前已释放且无其他等待者, 一并回收锁登记.
func (c *Cache) leaveWaiter(key [32]byte, lock *keyLock) {
	c.mu.Lock()
	lock.refs--
	if lock.refs == 0 {
		delete(c.locks, key)
	}
	c.mu.Unlock()
}

// unlock 清除持有者并将令牌移交给下一位等待者; 若已无等待者则直接删除锁登记, 锁表大小只与在途请求键相关.
func (c *Cache) unlock(key [32]byte, lock *keyLock) {
	c.mu.Lock()
	lock.owner = nil
	lock.refs--
	if lock.refs == 0 {
		delete(c.locks, key)
		c.mu.Unlock()
		return
	}
	lock.ch <- struct{}{}
	c.mu.Unlock()
}

// passing 判断键是否处于直通窗口. 以存入的单调时钟截止时刻复核, 墙钟回拨不会延长窗口.
func (c *Cache) passing(key [32]byte) bool {
	until, ok := c.passes.Get(key)
	return ok && time.Now().Before(until)
}

// fresh 只读检查键是否已有未过期条目, 不更新 LRU 次序, 供持锁者释放时判断响应是否已准入.
func (c *Cache) fresh(key [32]byte) bool {
	entry, ok := c.store.Peek(key)
	return ok && time.Now().Before(entry.expiresAt)
}

// releasePass 是直通及重入请求的释放函数. 它们不持有键锁, 使用具名函数避免每次分配闭包.
func releasePass(bool) {}

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
