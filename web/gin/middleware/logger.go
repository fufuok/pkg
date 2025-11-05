package middleware

import (
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/fufuok/utils/xjson/jsongen"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
)

// LogCondition 日志记录条件计算器
type LogCondition func(c *gin.Context, elapsed time.Duration) bool

func DefaultLogCondition(c *gin.Context, elapsed time.Duration) bool {
	return config.Config().LogConf.PostAPI != "" &&
		(elapsed > config.WebLogSlowResponse || c.Writer.Status() >= config.WebLogMinStatusCode)
}

func AllLogCondition(*gin.Context, time.Duration) bool {
	return config.Config().LogConf.PostAPI != ""
}

// WebLogger Web 日志, 记录错误日志, 推送请求日志到接口
func WebLogger(cond LogCondition) gin.HandlerFunc {
	gin.Logger()
	if cond == nil {
		cond = DefaultLogCondition
	}
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		elapsed := time.Since(start)
		if len(c.Errors) > 0 {
			sampler.Error().Strs("errors", c.Errors.Errors()).Str("elapsed", elapsed.String()).
				Str("client_ip", c.ClientIP()).Str("uri", c.Request.RequestURI).
				Msg(c.Request.Method)
		}

		if !cond(c, elapsed) {
			return
		}

		// 待推送日志写入队列
		js := jsongen.NewMap()
		js.PutString("type", config.BinName)
		js.PutString("req_time", start.Format(time.RFC3339))
		js.PutString("req_method", c.Request.Method)
		js.PutString("req_host", c.Request.Host)
		js.PutString("req_uri", c.Request.RequestURI)
		js.PutString("req_proto", c.Request.Proto)
		js.PutString("req_ua", c.Request.UserAgent())
		js.PutString("req_referer", c.Request.Referer())
		js.PutString("req_client_ip", c.ClientIP())
		js.PutInt("req_size", c.Request.ContentLength)
		js.PutInt("resp_size", int64(c.Writer.Size()))
		js.PutInt("status_code", int64(c.Writer.Status()))
		js.PutInt("took", elapsed.Milliseconds())

		if len(c.Errors) > 0 {
			js.PutStringArray("errors", c.Errors.Errors())
		}

		info := c.GetString(config.WebLogInfoKey)
		if info != "" {
			js.PutString("info", info)
		}

		common.LogChan.In <- js.Serialize(nil)
	}
}

// RecoveryWithLog GinRecovery 及日志
// Ref: https://github.com/gin-contrib/zap
func RecoveryWithLog(stack bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			err := recover()
			if err == nil {
				return
			}

			// Check for a broken connection, as it is not really a
			// condition that warrants a panic stack trace.
			var brokenPipe bool
			if ne, ok := err.(*net.OpError); ok {
				var se *os.SyscallError
				if errors.As(ne.Err, &se) {
					if strings.Contains(strings.ToLower(se.Error()), "broken pipe") ||
						strings.Contains(strings.ToLower(se.Error()), "connection reset by peer") {
						brokenPipe = true
					}
				}
			}

			httpRequest, _ := httputil.DumpRequest(c.Request, false)
			if brokenPipe {
				sampler.Error().Bytes("request", httpRequest).Str("path", c.Request.URL.Path).
					Msgf("Recovery: %s", err)
				// If the connection is dead, we can't write a status to it.
				_ = c.Error(err.(error)) // nolint: errcheck
				c.Abort()
				return
			}

			if stack {
				sampler.Error().Bytes("stack", debug.Stack()).Bytes("request", httpRequest).
					Str("path", c.Request.URL.Path).Msgf("Recovery: %s", err)
			} else {
				sampler.Error().Bytes("request", httpRequest).Str("path", c.Request.URL.Path).
					Msgf("Recovery: %s", err)
			}
			c.AbortWithStatus(http.StatusInternalServerError)
		}()

		c.Next()
	}
}
