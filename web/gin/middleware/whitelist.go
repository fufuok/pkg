package middleware

import (
	"fmt"
	"net/http"

	"github.com/fufuok/utils"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/gin/response"
)

// CheckWhitelist 接口白名单检查
func CheckWhitelist(asAPI bool) gin.HandlerFunc {
	errMsg := fmt.Sprintf("[ERROR] 非法来访(%s): ", config.AppName)
	return func(c *gin.Context) {
		if len(config.Whitelist) > 0 {
			clientIP := c.ClientIP()
			if !utils.InIPNetString(clientIP, config.Whitelist) {
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
