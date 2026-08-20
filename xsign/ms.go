package xsign

import (
	"bytes"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
)

const SignFieldName = "sign"

// MSGenSign 算法: 拼串后 MD5
// md5(k1=v1&k2=v2&token)
// Ref: microservice/common
func MSGenSign(params any, token string) (raw, sign string) {
	raw, _ = MSGetSignRaw(params)
	sign = MSGenSignWithRaw(raw, token)
	return
}

func MSGenSignWithRaw(raw, token string) string {
	return xhash.MD5Hex(raw + token)
}

// MSGetSignRaw 拼接参数为待加密的字符串, 如: k1=v1&k2=v2&
// 如果参数中包含 `sign` 字段会填充到第二个返回值
func MSGetSignRaw(params any) (raw, sign string) {
	if assert.IsEmpty(params) {
		return
	}

	switch val := params.(type) {
	default:
	case map[string]string:
		return buildMapString(val)
	case map[string]any:
		return buildMapAny(val)
	case url.Values:
		tm := map[string]string{}
		for k, vs := range val {
			tm[k] = strings.Join(vs, ",")
		}
		return buildMapString(tm)
	}

	// 尝试把数据转换成 map
	m := make(map[string]any)
	bs := json.MustJSON(params)
	dec := json.NewDecoder(bytes.NewReader(bs))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return
	}
	return MSGetSignRaw(m)
}

// MSVerifySign 用参数中的 `sign` 字段值校验由此参数生成的签名
func MSVerifySign(params any, token string) (raw string, match bool) {
	raw, signValue := MSGetSignRaw(params)
	if signValue == "" {
		return
	}
	match = strings.EqualFold(signValue, xhash.MD5Hex(raw+token))
	return
}

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

	s := strings.Builder{}
	for i := range keys {
		fmt.Fprintf(&s, "%s=%s&", keys[i], m[keys[i]])
	}
	raw = s.String()
	return
}

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

	s := strings.Builder{}
	for i := range keys {
		fmt.Fprintf(&s, "%s=%v&", keys[i], m[keys[i]])
	}
	raw = s.String()
	return
}
