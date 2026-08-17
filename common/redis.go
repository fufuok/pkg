package common

import (
	"context"
	"sort"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// !!! 注意: 先执行 InitRedisDB(...) 初始化后再使用下面的方法

var (
	// RedisDB Redis 连接
	RedisDB redis.UniversalClient

	// RedisDBInited RedisDB 是否已经初始化
	RedisDBInited atomic.Bool
)

// InitRedisDB 指定已初始化的 *redis.Client
func InitRedisDB(rdb redis.UniversalClient) {
	RedisDB = rdb
	RedisDBInited.Store(rdb != nil)
}

// TryLock 简单锁, 过期机制, 不主动解锁
func TryLock(key string, ttl time.Duration) bool {
	if !RedisDBInited.Load() {
		return false
	}
	return RedisDB.SetNX(context.Background(), key, "", ttl).Val()
}

// LockKeyTTL 锁的剩余生命周期
func LockKeyTTL(key string) time.Duration {
	if !RedisDBInited.Load() {
		return 0
	}
	return RedisDB.PTTL(context.Background(), key).Val()
}

// ClockOffsetChanRedis 基于 Redis, 周期性获取时钟偏移值.
// interval 等待和向容量 1 通道发送都会观察 ctx, 取消后关闭通道并退出.
func ClockOffsetChanRedis(ctx context.Context, interval time.Duration, rdb redis.UniversalClient) chan time.Duration {
	if rdb == nil {
		return nil
	}

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
			if ctx.Err() != nil {
				return
			}
			start := time.Now()
			if t, err := rdb.Time(ctx).Result(); err == nil {
				rtt := time.Since(start)
				offset := time.Until(t) + rtt/2
				offsets = append(offsets, int(offset))
				if len(offsets) == 3 {
					// 去头尾, 取中间值; 发送也观察 ctx, 避免容量 1 通道在无人消费时卡住退出.
					sort.Ints(offsets)
					if !sendClockOffset(ctx, ch, time.Duration(offsets[1])) {
						return
					}
					offsets = offsets[:0]
				}
			}
			// ticker 等待必须可取消, 生产周期默认 2h, 否则 stopTimeSync 只能干等下一拍.
			if !waitTickerOrDone(ctx, ticker.C) {
				return
			}
		}
	}()
	return ch
}

// sendClockOffset 向容量 1 的偏移通道发送一次结果.
// 通道满时继续等消费方, 只有 ctx 取消才放弃, 避免把三点中值丢掉.
func sendClockOffset(ctx context.Context, ch chan<- time.Duration, offset time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- offset:
		return true
	}
}

// waitTickerOrDone 等待下一拍或 ctx 取消.
// 只读 ticker.C 会让热更新/Stop 卡在整个 interval 上.
func waitTickerOrDone(ctx context.Context, tickerC <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-tickerC:
		return true
	}
}
