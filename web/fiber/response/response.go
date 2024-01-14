package response

import (
	"strconv"

	"github.com/fufuok/utils"
)

var (
	// 减少 JSON 序列化, 用于拼接 JSON Bytes 数据
	okA = []byte(`{"ok":1,"code":0,"msg":"","data":`)
	okB = []byte(`,"count":`)
	okC = []byte(`}`)
)

// APIData API 标准返回, 内部规范
// id: 1, ok: 1, code: 0 成功; id: 0, ok: 0, code: 1 失败
// 成功时 msg 必定为空
type APIData struct {
	OK    int    `json:"ok"`
	Code  int    `json:"code"`
	Msg   string `json:"msg"`
	Data  any    `json:"data"`
	Count int    `json:"count"`
}

// APIFailureData API 请求失败返回值
func APIFailureData(msg string, data any) *APIData {
	return &APIData{
		OK:    0,
		Code:  1,
		Msg:   msg,
		Data:  data,
		Count: 0,
	}
}

// APISuccessData API 请求成功返回值
func APISuccessData(data any, count int) *APIData {
	return &APIData{
		OK:    1,
		Code:  0,
		Msg:   "",
		Data:  data,
		Count: count,
	}
}

// APISuccessBytesData API 请求成功返回值(JSON Bytes)
func APISuccessBytesData(data []byte, count int) []byte {
	n := utils.S2B(strconv.Itoa(count))
	return utils.JoinBytes(okA, data, okB, n, okC)
}

// APISuccessNilData API 请求成功返回, 无数据
func APISuccessNilData() *APIData {
	return &APIData{
		OK:    1,
		Code:  0,
		Msg:   "",
		Data:  nil,
		Count: 0,
	}
}
