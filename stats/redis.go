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

// RedisInfo Redis 运行状态信息
func RedisInfo() map[string]any {
	if !common.RedisDBInited.Load() {
		return nil
	}

	ret := make(map[string]any)
	info := common.RedisDB.Info(context.Background()).Val()
	for _, v := range strings.Split(info, "\n") {
		v = strings.TrimSpace(v)
		if v == "" || strings.HasPrefix(v, "#") {
			continue
		}
		items := strings.SplitN(v, ":", 2)
		ret[items[0]] = items[1]
	}
	return ret
}
