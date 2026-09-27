package stats

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/fufuok/pkg/common"
)

// RedisStats Redis 连接池统计信息
func RedisStats() map[string]any {
	if !common.RedisDBInited.Load() {
		return nil
	}

	poolStats := common.RedisDB.PoolStats()
	stats := map[string]any{
		"Hits":       poolStats.Hits,
		"Misses":     poolStats.Misses,
		"Timeouts":   poolStats.Timeouts,
		"TotalConns": poolStats.TotalConns,
		"IdleConns":  poolStats.IdleConns,
		"StaleConns": poolStats.StaleConns,
		"DBSize":     RedisDBSize(),
	}
	// Cluster, Ring 等 UniversalClient 没有单机 Options. 只有普通客户端补这三项,
	// 其余实现仍返回通用池统计, 不能因为类型断言失败而 panic.
	if client, ok := common.RedisDB.(*redis.Client); ok {
		options := client.Options()
		stats["PoolSize"] = options.PoolSize
		stats["Addr"] = options.Addr
		stats["DB"] = options.DB
	}
	return stats
}

// RedisDBSize 当前数据库键数量
func RedisDBSize() int {
	if !common.RedisDBInited.Load() {
		return -1
	}

	n, err := common.RedisDB.DBSize(context.Background()).Result()
	if err != nil {
		return -1
	}
	return int(n)
}

// RedisInfo Redis 运行状态信息.
// 未初始化返回 nil; INFO 中空行、注释和无冒号行会被跳过, 不因此 panic.
func RedisInfo() map[string]any {
	if !common.RedisDBInited.Load() {
		return nil
	}

	ret := make(map[string]any)
	info := common.RedisDB.Info(context.Background()).Val()
	for v := range strings.SplitSeq(info, "\n") {
		v = strings.TrimSpace(v)
		if v == "" || strings.HasPrefix(v, "#") {
			continue
		}
		items := strings.SplitN(v, ":", 2)
		// Redis INFO 偶发无冒号行, 缺 value 时跳过, 避免公开 /sys 接口 panic.
		if len(items) != 2 {
			continue
		}
		ret[items[0]] = items[1]
	}
	return ret
}
