package middleware

import (
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/fufuok/freelru"

	"github.com/fufuok/pkg/common"
)

type ipCache struct {
	lru      freelru.Cache[string, ipVerdict]
	capacity uint32
	lifetime uint32
}

// ipVerdict 是一次黑白名单匹配的缓存结果, 绑定计算它所用的名单快照.
// 名单热更新会发布新的快照 map, 旧快照算出的结果因标识不同而不再命中.
type ipVerdict struct {
	// rules 是计算时名单快照 map 的标识. 存指针而不是 uintptr: 条目持有旧快照引用,
	// 旧 map 在条目淘汰前不会被回收, 其地址不可能被新快照复用而误判为同一快照.
	rules unsafe.Pointer
	ok    bool
}

var cacheMu sync.Mutex

// swapIPCache 在配置串行锁下更新或复用黑白名单 LRU 缓存, 请求侧通过 atomic.Pointer 无锁读取快照.
// 容量与生命周期未变化时直接清空并复用既有分片缓存, 避免热重载重分配大块切片及产生接口并发写竞争.
func swapIPCache(slot *atomic.Pointer[ipCache], capacity, lifetime uint32) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if capacity == 0 {
		if old := slot.Swap(nil); old != nil {
			old.lru.Purge()
		}
		return nil
	}

	if cur := slot.Load(); cur != nil && cur.capacity == capacity && cur.lifetime == lifetime {
		cur.lru.Purge()
		return nil
	}

	lru, err := freelru.NewShardedDefault[string, ipVerdict](capacity, time.Duration(lifetime)*time.Second)
	if err != nil {
		if old := slot.Swap(nil); old != nil {
			old.lru.Purge()
		}
		return err
	}

	if old := slot.Swap(&ipCache{
		lru:      lru,
		capacity: capacity,
		lifetime: lifetime,
	}); old != nil {
		old.lru.Purge()
	}
	return nil
}

// lookupIPList 报告 ip 是否命中名单快照 rules, cache 非 nil 时先查后填 LRU 缓存.
// 缓存条目只在其所属快照仍是本次请求读到的快照时命中: 名单热更新后旧结果立即失效, 不依赖应用及时 Purge;
// 更新前已读旧快照的在途请求即使在 Purge 之后才回填, 写入的也是旧快照标识, 新快照下的请求不会采用.
// rules 为空时调用方应已短路返回, 这里不再特判.
func lookupIPList(cache *ipCache, rules map[*net.IPNet]int64, ip string) bool {
	if cache == nil {
		_, ok := common.LookupIPNetsString(ip, rules)
		return ok
	}
	// map 本身是指针形态, 取其标识不分配.
	id := reflect.ValueOf(rules).UnsafePointer()
	if v, loaded := cache.lru.Get(ip); loaded && v.rules == id {
		return v.ok
	}
	_, ok := common.LookupIPNetsString(ip, rules)
	cache.lru.Add(ip, ipVerdict{rules: id, ok: ok})
	return ok
}

func CacheStats() map[string]any {
	m := make(map[string]any)
	if c := whitelistCache.Load(); c != nil {
		m["Whitelist"] = c.lru.Metrics()
	}
	if c := blacklistCache.Load(); c != nil {
		m["Blacklist"] = c.lru.Metrics()
	}
	return m
}
