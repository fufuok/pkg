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
	redisOptions := common.RedisDB.(*redis.Client).Options()
	return map[string]any{
		"Hits":       poolStats.Hits,
		"Misses":     poolStats.Misses,
		"Timeouts":   poolStats.Timeouts,
		"TotalConns": poolStats.TotalConns,
		"IdleConns":  poolStats.IdleConns,
		"StaleConns": poolStats.StaleConns,
		"PoolSize":   redisOptions.PoolSize,
		"Addr":       redisOptions.Addr,
		"DB":         redisOptions.DB,
		"DBSize":     RedisDBSize(),
	}
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
