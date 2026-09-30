package utils

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// S2B 返回字符串底层数据的零拷贝字节视图, 长度和容量均等于字符串长度.
// 返回切片只可读取, 不得修改; 需要可写的独立副本时使用 CopyS2B.
// 空字符串返回零长度切片, 是否为 nil 由字符串的底层表示决定.
func S2B(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// B2S 返回字节切片底层数据的零拷贝字符串视图, nil 或空切片返回空字符串.
// 字符串仍在使用时, 调用方不得修改或归还底层字节到对象池; 需要独立副本时使用 CopyB2S.
// 直接复用切片头的数据指针和长度; Go 1.27/amd64 基准中 unsafe.String 写法仍有额外检查开销.
func B2S(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// MustJSONIndent 转 json 返回 []byte
func MustJSONIndent(v any) []byte {
	js, _ := json.MarshalIndent(v, "", "  ")
	return js
}

// MustJSONIndentString 转 json Indent 返回 string
func MustJSONIndentString(v any) string {
	return B2S(MustJSONIndent(v))
}

// MustJSON 转 json 返回 []byte
func MustJSON(v any) []byte {
	js, _ := json.Marshal(v)
	return js
}

// MustJSONString 转 json 返回 string
func MustJSONString(v any) string {
	return B2S(MustJSON(v))
}

// MustString 强制转为字符串
func MustString(v any, timeLayout ...string) string {
	switch s := v.(type) {
	default:
		return fmt.Sprint(v)
	case string:
		return s
	case []byte:
		return string(s)
	case error:
		return s.Error()
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(s)
	case int:
		return strconv.Itoa(s)
	case int8:
		return strconv.FormatInt(int64(s), 10)
	case int16:
		return strconv.FormatInt(int64(s), 10)
	case int32:
		return strconv.Itoa(int(s))
	case int64:
		return strconv.FormatInt(s, 10)
	case uint:
		return strconv.FormatUint(uint64(s), 10)
	case uint8:
		return strconv.FormatUint(uint64(s), 10)
	case uint16:
		return strconv.FormatUint(uint64(s), 10)
	case uint32:
		return strconv.FormatUint(uint64(s), 10)
	case uint64:
		return strconv.FormatUint(s, 10)
	case float32:
		return strconv.FormatFloat(float64(s), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case time.Time:
		if len(timeLayout) > 0 {
			return s.Format(timeLayout[0])
		}
		return s.Format("2006-01-02 15:04:05")
	case reflect.Value:
		return MustString(s.Interface(), timeLayout...)
	case fmt.Stringer:
		return s.String()
	}
}

// MustInt 强制转为整数 (int)
func MustInt(v any) int {
	switch i := v.(type) {
	default:
		d, ok := i.(int)
		if ok {
			return d
		}
		return 0
	case string:
		v, err := strconv.Atoi(strings.TrimSpace(i))
		if err == nil {
			return v
		}
		return 0
	case bool:
		if i {
			return 1
		}
		return 0
	case nil:
		return 0
	case int:
		return i
	case int8:
		return int(i)
	case int16:
		return int(i)
	case int32:
		return int(i)
	case int64:
		return int(i)
	case uint:
		return int(i)
	case uint8:
		return int(i)
	case uint16:
		return int(i)
	case uint32:
		return int(i)
	case uint64:
		return int(i)
	case float32:
		return int(i)
	case float64:
		return int(i)
	}
}

// MustBool 强制转为 bool
func MustBool(v any) bool {
	switch t := v.(type) {
	default:
		if MustInt(v) != 0 {
			return true
		}
	case bool:
		return t
	case string:
		switch t {
		case "1", "t", "T", "true", "TRUE", "True":
			return true
		}
	}

	return false
}

// B64Encode Base64 编码
func B64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// B64Decode Base64 解码
func B64Decode(s string) []byte {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b
	}

	return nil
}

// B64UrlEncode Base64 解码, 安全 URL, 替换: "+/" 为 "-_"
func B64UrlEncode(b []byte) string {
	return base64.URLEncoding.EncodeToString(b)
}

// B64UrlDecode Base64 解码
func B64UrlDecode(s string) []byte {
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b
	}

	return nil
}
