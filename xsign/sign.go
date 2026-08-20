// Package xsign 提供微服务请求参数拼串 MD5 签名.
//
// 算法来自生产侧 microservice/common: md5(k1=v1&k2=v2&token).
// 这不是加密, 也不是 common.GenSign 的 ts+key 签名, 更不能替换 xcrypto.Encrypt / Seal.
package xsign

import (
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
)

const (
	// SignFieldName 参数中的签名字段名, 拼串时抽出, 不进入摘要.
	SignFieldName = "sign"
)

// GenSign 按参数拼串后追加 token 做 MD5.
// 空值、非对象和无法转成 map 的输入得到 md5(token), 与历史金值一致.
func GenSign(params any, token string) (raw, sign string) {
	raw, _ = GetSignRaw(params)
	sign = GenSignWithRaw(raw, token)
	return
}

// GenSignWithRaw 对已拼好的 raw 追加 token 后做 MD5Hex.
func GenSignWithRaw(raw, token string) string {
	return xhash.MD5Hex(raw + token)
}

// GetSignRaw 拼接待签名字符串, 形如 k1=v1&k2=v2&.
// 空字符串和 nil 不进拼串; 名为 sign 的字段只抽出到第二个返回值.
func GetSignRaw(params any) (raw, sign string) {
	if isEmpty(params) {
		return
	}

	switch val := params.(type) {
	case map[string]string:
		return buildMapString(val)
	case map[string]any:
		return buildMapAny(val)
	case url.Values:
		tm := make(map[string]string, len(val))
		for k, vs := range val {
			tm[k] = strings.Join(vs, ",")
		}
		return buildMapString(tm)
	}

	// 结构体等非 map 输入先 JSON 再解成 map, 数字保持 json.Number, 避免 float 精度漂移.
	m := make(map[string]any)
	bs := json.MustJSON(params)
	dec := json.NewDecoder(bytes.NewReader(bs))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return
	}
	return GetSignRaw(m)
}

// VerifySign 用参数中的 sign 字段校验拼串 MD5, 十六进制大小写不敏感.
// 缺少 sign 时 match 为 false.
func VerifySign(params any, token string) (raw string, match bool) {
	raw, signValue := GetSignRaw(params)
	if signValue == "" {
		return
	}
	match = strings.EqualFold(signValue, xhash.MD5Hex(raw+token))
	return
}

// buildMapString 按 key 排序拼接 map[string]string, 空值和 sign 字段不进 raw.
func buildMapString(m map[string]string) (raw, sign string) {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		if k == SignFieldName {
			sign = v
			continue
		}
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var s strings.Builder
	for i := range keys {
		fmt.Fprintf(&s, "%s=%s&", keys[i], m[keys[i]])
	}
	raw = s.String()
	return
}

// buildMapAny 按 key 排序拼接 map[string]any, nil 和空字符串跳过, 其余用 %v.
func buildMapAny(m map[string]any) (raw, sign string) {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == "" || v == nil {
			continue
		}
		if k == SignFieldName {
			sign = utils.MustString(v)
			continue
		}
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var s strings.Builder
	for i := range keys {
		fmt.Fprintf(&s, "%s=%v&", keys[i], m[keys[i]])
	}
	raw = s.String()
	return
}

// isEmpty 判断签名输入是否视为空. 空输入不拼串, 最终摘要为 md5(token).
func isEmpty(o any) bool {
	if o == nil {
		return true
	}

	v := reflect.ValueOf(o)
	switch v.Kind() {
	case reflect.Chan, reflect.Map, reflect.Slice:
		return v.Len() == 0
	case reflect.Pointer:
		if v.IsNil() {
			return true
		}
		return isEmpty(v.Elem().Interface())
	default:
		zero := reflect.Zero(v.Type())
		return reflect.DeepEqual(o, zero.Interface())
	}
}
