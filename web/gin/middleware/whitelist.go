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

type ForbiddenChecker = func(*gin.Context) bool

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
		_, ok := common.LookupIPNetsString(clientIP, config.Whitelist)
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
