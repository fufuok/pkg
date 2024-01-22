package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/json"
)

const (
	defaultErrMsg = "错误的请求"
)

var apiSuccessNil = json.MustJSON(APISuccessNilData())

// APIException 通用异常处理
func APIException(c *gin.Context, code int, msg string, data any) {
	if msg == "" {
		msg = defaultErrMsg
	}
	c.AbortWithStatusJSON(code, APIFailureData(msg, data))
}

// APIFailure 返回失败, 状态码: 400
func APIFailure(c *gin.Context, msg string, data any) {
	APIException(c, http.StatusBadRequest, msg, data)
}

// APISuccess 返回成功, 状态码: 200
func APISuccess(c *gin.Context, data any, count int) {
	c.JSON(http.StatusOK, APISuccessData(data, count))
}

// APISuccessBytes 返回成功, JSON 字节数据, 状态码: 200
func APISuccessBytes(c *gin.Context, data []byte, count int) {
	c.Data(http.StatusOK, MIMEApplicationJSONCharsetUTF8, APISuccessBytesData(data, count))
}

// APISuccessNil 返回成功, 无数据, 状态码: 200
func APISuccessNil(c *gin.Context) {
	c.Data(http.StatusOK, MIMEApplicationJSONCharsetUTF8, apiSuccessNil)
}

// TxtException 异常处理, 文本消息
func TxtException(c *gin.Context, code int, msg string) {
	if msg == "" {
		msg = defaultErrMsg
	}
	c.String(code, msg)
	c.Abort()
}

// TxtMsg 返回文本消息
func TxtMsg(c *gin.Context, msg string) {
	c.String(http.StatusOK, msg)
}
