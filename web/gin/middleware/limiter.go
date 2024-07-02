package middleware

import (
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/gin/response"
)

const (
	XRateLimitLimit     = "X-RateLimit-Limit"
	XRateLimitRemaining = "X-RateLimit-Remaining"
	XRateLimitReset     = "X-RateLimit-Reset"
)

var (
	// DefaultLimitMessage 触发限制时的 429 消息
	DefaultLimitMessage = "处理中的请求数过多"

	// DefaultLimitReached 达到限制时执行的 Hook
	DefaultLimitReached = func(c *gin.Context) {
		response.APIException(c, http.StatusTooManyRequests, DefaultLimitMessage, nil)
	}
)

// RequestsLimiter 同时处理的请求数限制
// 请求处理中 +1, 请求已返回: -1
// 请求超限返回 429 错误
type RequestsLimiter struct {
	limit   atomic.Int32
	running atomic.Int32
	limited atomic.Uint64

	limitReached gin.HandlerFunc
}

func (r *RequestsLimiter) Allow() bool {
	if r.limit.Load() < 0 {
		return true
	}

	for {
		n := r.running.Load()
		if n >= r.limit.Load() {
			return false
		}
		if r.running.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

func (r *RequestsLimiter) Stats() map[string]int {
	return map[string]int{
		"Limit":     int(r.Limit()),
		"Limited":   int(r.Limited()),
		"Running":   int(r.Running()),
		"Remaining": int(r.Remaining()),
	}
}

func (r *RequestsLimiter) Limited() uint64 {
	return r.limited.Load()
}

func (r *RequestsLimiter) SetLimit(n int32) {
	r.limit.Store(n)
}

func (r *RequestsLimiter) Limit() int32 {
	return r.limit.Load()
}

func (r *RequestsLimiter) Running() int32 {
	return r.running.Load()
}

func (r *RequestsLimiter) Remaining() int32 {
	n := r.limit.Load()
	if n < 0 {
		return 1
	}
	return n - r.running.Load()
}

func (r *RequestsLimiter) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !r.Allow() {
			r.limited.Add(1)
			r.limitReached(c)
			return
		}

		defer r.running.Add(-1)
		c.Header(XRateLimitLimit, strconv.FormatInt(int64(r.Limit()), 10))
		c.Header(XRateLimitRemaining, strconv.FormatInt(int64(r.Remaining()), 10))
		c.Next()
	}
}

// NewDefaultRequestsLimiter 使用配置文件参数创建限制器
// app.Use(middleware.NewDefaultRequestsLimiter().Handler())
func NewDefaultRequestsLimiter() *RequestsLimiter {
	return NewRequestsLimiter(config.Config().WebConf.RequestsLimit, DefaultLimitReached)
}

func NewRequestsLimiter(limit int32, limitReached gin.HandlerFunc) *RequestsLimiter {
	if limitReached == nil {
		limitReached = DefaultLimitReached
	}
	r := &RequestsLimiter{
		limitReached: limitReached,
	}
	r.limit.Store(limit)
	return r
}
