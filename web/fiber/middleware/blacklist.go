package middleware

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/fufuok/freelru"
	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/fiber/proxy"
)

var (
	// 存放最近检查的黑名单 IP
	blacklistLRU    freelru.Cache[string, bool]
	useBlacklistLRU atomic.Bool
)

// UseBlacklistCache 重新设置黑名单检查时缓存, 配置变化时可选再次调用, 由应用端 Start() Runtime() 调用
func UseBlacklistCache(capacity, lifetime uint64) (err error) {
	if capacity == 0 {
		return nil
	}

	lru, err := freelru.NewShardedDefault[string, bool](capacity, time.Duration(lifetime)*time.Second)
	if err != nil {
		return err
	}

	useBlacklistLRU.Store(false)
	blacklistLRU = lru
	useBlacklistLRU.Store(blacklistLRU != nil)
	return nil
}

// PurgeBlacklistCache 配置项变化时需要清空缓存, 由应用端在 Runtime() 调用
func PurgeBlacklistCache() {
	if blacklistLRU != nil {
		blacklistLRU.Purge()
	}
}

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) fiber.Handler {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *fiber.Ctx) error {
		if BlacklistChecker(c) {
			return responseForbidden(c, errMsg, asAPI)
		}
		return c.Next()
	}
}

// BlacklistChecker 是否存在于黑名单, true 是黑名单 (黑名单为空时: 放过, false)
func BlacklistChecker(c *fiber.Ctx) bool {
	clientIP := proxy.GetClientIP(c)
	if len(config.Blacklist) > 0 {
		if useBlacklistLRU.Load() {
			if ok, loaded := blacklistLRU.Get(clientIP); loaded {
				return ok
			}
		}
		_, ok := common.LookupIPNetsString(clientIP, config.Blacklist)
		if useBlacklistLRU.Load() {
			blacklistLRU.Add(clientIP, ok)
		}
		return ok
	}
	return false
}
