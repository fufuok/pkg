package middleware

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/fufuok/freelru"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/gin/response"
)

type ForbiddenChecker = func(*gin.Context) bool

var (
	// 存放最近检查的白名单 IP
	whitelistLRU    freelru.Cache[string, bool]
	useWhitelistLRU atomic.Bool
)

// UseWhitelistCache 重新设置白名单检查时缓存, 配置变化时可选再次调用, 由应用端 Start() Runtime() 调用
func UseWhitelistCache(capacity, lifetime uint32) error {
	useWhitelistLRU.Store(false)
	if capacity == 0 {
		return nil
	}

	lru, err := freelru.NewShardedDefault[string, bool](capacity, time.Duration(lifetime)*time.Second)
	if err != nil {
		return err
	}

	whitelistLRU = lru
	useWhitelistLRU.Store(whitelistLRU != nil)
	return nil
}

// PurgeWhitelistCache 配置项变化时需要清空缓存, 由应用端在 Runtime() 调用
func PurgeWhitelistCache() {
	if whitelistLRU != nil {
		whitelistLRU.Purge()
	}
}

// CheckWhitelist 接口白名单检查
func CheckWhitelist(asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 非法来访(%s): ", config.AppName)
	return func(c *gin.Context) {
		if !WhitelistChecker(c) {
			responseForbidden(c, errMsg, asAPI)
			return
		}
		c.Next()
	}
}

// CheckWhitelistOr 校验接口白名单或自定义检查器
func CheckWhitelistOr(checker ForbiddenChecker, asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 禁止来访(%s): ", config.AppName)
	return func(c *gin.Context) {
		if !WhitelistChecker(c) && !checker(c) {
			responseForbidden(c, errMsg, asAPI)
			return
		}
		c.Next()
	}
}

// CheckWhitelistAnd 同时校验接口白名单和自定义检查器
func CheckWhitelistAnd(checker ForbiddenChecker, asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 禁止访问(%s): ", config.AppName)
	return func(c *gin.Context) {
		if !WhitelistChecker(c) || !checker(c) {
			responseForbidden(c, errMsg, asAPI)
			return
		}
		c.Next()
	}
}

// WhitelistChecker 是否通过了白名单检查, true 是白名单 (白名单为空时: 通过, true)
func WhitelistChecker(c *gin.Context) bool {
	clientIP := c.ClientIP()
	if len(config.Whitelist) > 0 {
		if useWhitelistLRU.Load() {
			if ok, loaded := whitelistLRU.Get(clientIP); loaded {
				return ok
			}
		}
		_, ok := common.LookupIPNetsString(clientIP, config.Whitelist)
		if useWhitelistLRU.Load() {
			whitelistLRU.Add(clientIP, ok)
		}
		return ok
	}
	return true
}

func responseForbidden(c *gin.Context, msg string, asAPI bool) {
	clientIP := c.ClientIP()
	msg += clientIP
	sampler.Info().
		Str("cip", clientIP).Str("x_forwarded_for", c.GetHeader("X-Forwarded-For")).
		Str("method", c.Request.Method).Str("uri", c.Request.RequestURI).
		Msg(msg)

	if asAPI {
		response.APIException(c, http.StatusForbidden, msg, nil)
	} else {
		response.TxtException(c, http.StatusForbidden, msg)
	}
}
