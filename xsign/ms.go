package xsign

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// MSGenHMACSign 对字符串字段用 url.Values.Encode 生成无歧义待签消息并计算 HMAC-SHA256.
// 字段名排序, 同名值保持原顺序, 空字符串参与签名; 无值切片遵从 Encode 语义被忽略.
// params 的所有字段均参与编码, 签名须在字段集合之外传输; 密钥非空和业务字段约束由调用方保证.
// 此编码与旧 MSGenSign 拼串协议不同, 两端必须共同采用新规则, 调用期间不得并发修改 params.
func MSGenHMACSign(params url.Values, secret string) (raw, sign string) {
	raw = params.Encode()
	return raw, MSGenHMACSignWithRaw(raw, secret)
}

// MSGenHMACSignWithRaw 对调用方给出的原始消息计算 HMAC-SHA256, 密钥为 secret.
// 返回 64 位小写十六进制; 此函数不规范化字段, 调用方必须保证消息与业务输入一一对应.
// 调用方还须校验密钥非空; 旧 MSGetSignRaw 仅适用于已约束字段, 不应直接用于任意请求参数或复杂对象.
func MSGenHMACSignWithRaw(raw, secret string) string {
	return xhash.HmacSHA256Hex(raw, secret)
}

// MSVerifyHMACSign 校验同一 url.Values.Encode 规则的签名, 不接受旧 MS 拼串签名.
// sign 独立传入, 不从 params 抽取或删除字段; 非法十六进制或不匹配都返回 false.
// 格式非法时 raw 为空; 其他情况返回参与校验的编码, 不应直接回显到外部响应.
// 字段及密钥约束与 MSGenHMACSign 相同, 比较使用 hmac.Equal.
func MSVerifyHMACSign(params url.Values, sign, secret string) (raw string, match bool) {
	if len(sign) != sha256.Size*2 {
		return "", false
	}
	// 摘要长度固定, 在栈上解码以避免验签热路径为十六进制结果分配切片.
	var got [sha256.Size]byte
	if _, err := hex.Decode(got[:], utils.S2B(sign)); err != nil {
		return "", false
	}
	raw = params.Encode()
	return raw, hmac.Equal(got[:], xhash.HmacSHA256(utils.S2B(raw), utils.S2B(secret)))
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
