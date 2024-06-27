package middleware

import (
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/web/fiber/response"
)

const (
	XRateLimitLimit     = "X-RateLimit-Limit"
	XRateLimitRemaining = "X-RateLimit-Remaining"
	XRateLimitReset     = "X-RateLimit-Reset"
)

// DefaultLimitMessage 触发限制时的 429 消息
var DefaultLimitMessage = "处理中的请求数过多"

// RequestsLimiter 同时处理的请求数限制
// 请求处理中 +1, 请求已返回: -1
// 请求超限返回 429 错误
type RequestsLimiter struct {
	count   atomic.Uint64
	limit   atomic.Int32
	running atomic.Int32

	// 达到限制时的响应错误消息
	msg string
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
			r.count.Add(1)
			return true
		}
	}
}

func (r *RequestsLimiter) Count() uint64 {
	return r.count.Load()
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

func (r *RequestsLimiter) Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !r.Allow() {
			return response.APIException(c, http.StatusTooManyRequests, r.msg, nil)
		}

		defer r.running.Add(-1)
		c.Set(XRateLimitLimit, strconv.FormatInt(int64(r.Limit()), 10))
		c.Set(XRateLimitRemaining, strconv.FormatInt(int64(r.Remaining()), 10))
		return c.Next()
	}
}

// NewDefaultRequestsLimiter 使用配置文件参数创建限制器
// app.Use(middleware.NewDefaultRequestsLimiter().Handler())
func NewDefaultRequestsLimiter() *RequestsLimiter {
	return NewRequestsLimiter(config.Config().WebConf.RequestsLimit, DefaultLimitMessage)
}

func NewRequestsLimiter(limit int32, msg string) *RequestsLimiter {
	if msg == "" {
		msg = DefaultLimitMessage
	}
	r := &RequestsLimiter{
		msg: msg,
	}
	r.limit.Store(limit)
	return r
}
