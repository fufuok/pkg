package middleware

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/fufuok/freelru"
)

type ipCache struct {
	lru      freelru.Cache[string, bool]
	capacity uint32
	lifetime uint32
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

	lru, err := freelru.NewShardedDefault[string, bool](capacity, time.Duration(lifetime)*time.Second)
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
