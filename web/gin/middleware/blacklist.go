package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/gin/response"
)

// CheckBlacklist 接口黑名单检查
func CheckBlacklist(asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 非法访问(%s): ", config.AppName)
	return func(c *gin.Context) {
		if len(config.Blacklist) > 0 {
			clientIP := c.ClientIP()
			if _, ok := common.LookupIPNets(clientIP, config.Blacklist); ok {
				msg := errMsg + clientIP
				sampler.Info().
					Str("cip", clientIP).Str("x_forwarded_for", c.GetHeader("X-Forwarded-For")).
					Str("method", c.Request.Method).Str("uri", c.Request.RequestURI).
					Msg(msg)
				if asAPI {
					response.APIException(c, http.StatusForbidden, msg, nil)
				} else {
					response.TxtException(c, http.StatusForbidden, msg)
				}
				return
			}
		}

		c.Next()
	}
}
