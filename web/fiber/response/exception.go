package response

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/fufuok/pkg/json"
)

const defaultErrMsg = "错误的请求"

var apiSuccessNil = json.MustJSON(APISuccessNilData())

// APIException 通用异常处理
func APIException(c fiber.Ctx, code int, msg string, data any) error {
	if msg == "" {
		msg = defaultErrMsg
	}
	c.Status(code)
	return JSON(c, APIFailureData(msg, data))
}

// APIFailure 返回失败, 状态码: 400
func APIFailure(c fiber.Ctx, msg string, data any) error {
	return APIException(c, http.StatusBadRequest, msg, data)
}

// APISuccess 返回成功, 状态码: 200
func APISuccess(c fiber.Ctx, data any, count int) error {
	return JSON(c, APISuccessData(data, count))
}

// APISuccessBytes 返回成功, JSON 字节数据, 状态码: 200
func APISuccessBytes(c fiber.Ctx, data []byte, count int) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return c.Send(APISuccessBytesData(data, count))
}

// APISuccessNil 返回成功, 无数据, 状态码: 200
func APISuccessNil(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return c.Send(apiSuccessNil)
}

// TxtException 异常处理, 文本消息
func TxtException(c fiber.Ctx, code int, msg string) error {
	if msg == "" {
		msg = defaultErrMsg
	}
	c.Status(code)
	return c.SendString(msg)
}

// TxtMsg 返回文本消息
func TxtMsg(c fiber.Ctx, msg string) error {
	return c.SendString(msg)
}

// JSON 返回带 utf-8 的 JSON
func JSON(c fiber.Ctx, data any) error {
	err := c.JSON(data)
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
	return err
}
