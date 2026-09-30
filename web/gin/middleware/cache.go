package middleware

import (
	"bytes"
	"hash/maphash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chenyahui/gin-cache"
	"github.com/chenyahui/gin-cache/persist"
	"github.com/gin-gonic/gin"
)

const defaultCacheExpire = 10 * time.Minute

func shouldCompress(req *http.Request) bool {
	if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") ||
		strings.Contains(req.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		return false
	}
	return true
}

// CacheByRawData 以请求参数 Hash 值为键, 缓存请求结果
func CacheByRawData(cacheExpire time.Duration, forgetTimeout time.Duration, opts ...cache.Option) gin.HandlerFunc {
	if cacheExpire <= 0 {
		cacheExpire = defaultCacheExpire
	}
	memStore := persist.NewMemoryStore(cacheExpire)
	gzipKeys := map[bool]string{
		true:  "Y.",
		false: "N.",
	}
	// seed 绑定当前中间件实例, 保证同一实例中相同请求体得到稳定缓存键.
	// maphash 的种子只在当前进程和实例内有效, 不能跨实例或进程比较.
	// 必须在构造阶段生成, 不能在每个请求中重新 MakeSeed.
	seed := maphash.MakeSeed()
	opts = append(opts, cache.WithCacheStrategyByRequest(func(c *gin.Context) (bool, cache.Strategy) {
		body, err := c.GetRawData()
		defer func() {
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		}()
		if err != nil {
			return false, cache.Strategy{}
		}
		// gzip 格式结果单独缓存
		gzipKey := gzipKeys[shouldCompress(c.Request)]
		key := gzipKey + strconv.FormatUint(maphash.Bytes(seed, body), 10)
		return true, cache.Strategy{
			CacheKey: key,
		}
	}))
	opts = append(opts, cache.WithSingleFlightForgetTimeout(forgetTimeout))
	return cache.Cache(memStore, cacheExpire, opts...)
}

// CacheByRequestURI 以请求 URL 为键, 缓存请求结果
func CacheByRequestURI(cacheExpire time.Duration, forgetTimeout time.Duration, opts ...cache.Option) gin.HandlerFunc {
	if cacheExpire <= 0 {
		cacheExpire = defaultCacheExpire
	}
	memStore := persist.NewMemoryStore(cacheExpire)
	opts = append(opts, cache.WithCacheStrategyByRequest(func(c *gin.Context) (bool, cache.Strategy) {
		return true, cache.Strategy{
			CacheKey: c.Request.RequestURI,
		}
	}))
	opts = append(opts, cache.WithSingleFlightForgetTimeout(forgetTimeout))
	return cache.Cache(memStore, cacheExpire, opts...)
}

// CacheByRequestPath 以请求 URL.Path 为键, 缓存请求结果
func CacheByRequestPath(cacheExpire time.Duration, forgetTimeout time.Duration, opts ...cache.Option) gin.HandlerFunc {
	if cacheExpire <= 0 {
		cacheExpire = defaultCacheExpire
	}
	memStore := persist.NewMemoryStore(cacheExpire)
	opts = append(opts, cache.WithCacheStrategyByRequest(func(c *gin.Context) (bool, cache.Strategy) {
		return true, cache.Strategy{
			CacheKey: c.Request.URL.Path,
		}
	}))
	opts = append(opts, cache.WithSingleFlightForgetTimeout(forgetTimeout))
	return cache.Cache(memStore, cacheExpire, opts...)
}
