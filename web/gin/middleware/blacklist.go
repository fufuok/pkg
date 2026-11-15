package middleware

import (
	"fmt"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
)

// 存放最近检查的黑名单 IP 缓存快照
var blacklistCache atomic.Pointer[ipCache]

// UseBlacklistCache 重新设置黑名单检查时缓存, 配置变化时可选再次调用, 由应用端 Start() Runtime() 调用
func UseBlacklistCache(capacity, lifetime uint32) error {
	return swapIPCache(&blacklistCache, capacity, lifetime)
}

// PurgeBlacklistCache 清空黑名单缓存. 名单热更新后旧结果已按规则快照自动失效, 不再必须调用;
// 可在 Runtime() 中调用以尽早释放旧快照的缓存条目.
func PurgeBlacklistCache() {
	if c := blacklistCache.Load(); c != nil {
		c.lru.Purge()
	}
}

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *gin.Context) {
		if BlacklistChecker(c) {
			responseForbidden(c, errMsg, asAPI)
			return
		}
		c.Next()
	}
}

// BlacklistChecker 是否存在于黑名单, true 是黑名单 (黑名单为空的生产常态下直接跳过客户端 IP 解析).
func BlacklistChecker(c *gin.Context) bool {
	bl := config.BlacklistSnapshot()
	if len(bl) == 0 {
		return false
	}
	clientIP := c.ClientIP()
	// 缓存结果绑定名单快照, 热更新后旧结果 (含在途请求迟到的回填) 不会再放行或漏拦.
	return lookupIPList(blacklistCache.Load(), bl, clientIP)
}
