package common

import (
	"context"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// !!! 注意: 先执行 InitRedisDB(...) 初始化后再使用下面的方法

var (
	// RedisDB Redis 连接
	RedisDB redis.UniversalClient
)

// InitRedisDB 指定已初始化的 *redis.Client
func InitRedisDB(rdb redis.UniversalClient) {
	RedisDB = rdb
}

// TryLock 简单锁, 过期机制, 不主动解锁
func TryLock(key string, ttl time.Duration) bool {
	return RedisDB.SetNX(context.Background(), key, "", ttl).Val()
}

// LockKeyTTL 锁的剩余生命周期
func LockKeyTTL(key string) time.Duration {
	return RedisDB.PTTL(context.Background(), key).Val()
}

// ClockOffsetChanRedis 基于 Redis, 周期性获取时钟偏移值
func ClockOffsetChanRedis(ctx context.Context, interval time.Duration, rdb redis.UniversalClient) chan time.Duration {
	if interval == 0 {
		interval = ClockOffsetInterval
	} else if interval < ClockOffsetMinInterval {
		interval = ClockOffsetMinInterval
	}
	var offsets []int
	ch := make(chan time.Duration, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer func() {
			ticker.Stop()
			close(ch)
		}()
		for {
			select {
			default:
			case <-ctx.Done():
				return
			}
			start := time.Now()
			if t, err := rdb.Time(ctx).Result(); err == nil {
				rtt := time.Since(start)
				offset := time.Until(t) + rtt/2
				offsets = append(offsets, int(offset))
				if len(offsets) == 3 {
					// 去头尾, 取中间值
					sort.Ints(offsets)
					ch <- time.Duration(offsets[1])
					offsets = offsets[:0]
				}
			}
			<-ticker.C
		}
	}()
	return ch
}
