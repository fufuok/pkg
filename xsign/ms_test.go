package xsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestMSGenSign(t *testing.T) {
	key := "test.KEY-777"
	tests := []struct {
		param any
		want  string
	}{
		{nil, "df1f04e971d6ce284fa372fa81652e9e"},
		{struct{}{}, "df1f04e971d6ce284fa372fa81652e9e"},
		{"", "df1f04e971d6ce284fa372fa81652e9e"},
		{"123", "df1f04e971d6ce284fa372fa81652e9e"},
		{456, "df1f04e971d6ce284fa372fa81652e9e"},
		{
			map[string]any{
				"t":   1661834285,
				"a":   "test",
				"z":   true,
				"f":   3.14,
				"s":   "",
				"nil": nil,
			},
			"bc9d8821a9df04b663aaba90750eaa15",
		},
		{
			map[string]string{
				"zh":   "中　 文",
				"u":    "\\u3000",
				"sign": "ignore",
			},
			"64fdc52800ad3de0bef8e36d69a031f2",
		},
	}
	for _, d := range tests {
		_, got := MSGenSign(d.param, key)
		if got != d.want {
			t.Fatalf("got: %s, want: %s", got, d.want)
		}
	}
}

// TestMSGetSignRawSkipsEmptyAndSign 验证空值不进拼串, sign 字段只抽出不参与摘要.
func TestMSGetSignRawSkipsEmptyAndSign(t *testing.T) {
	raw, sign := MSGetSignRaw(map[string]string{
		"b":    "2",
		"a":    "1",
		"skip": "",
		"sign": "ABC",
	})
	if raw != "a=1&b=2&" {
		t.Fatalf("raw = %q", raw)
	}
	if sign != "ABC" {
		t.Fatalf("sign = %q", sign)
	}
}

// TestMSGetSignRawURLValuesJoinsWithComma 验证 url.Values 多值用逗号拼接后再按 key 排序.
func TestMSGetSignRawURLValuesJoinsWithComma(t *testing.T) {
	raw, sign := MSGetSignRaw(url.Values{
		"b":    []string{"2", "3"},
		"a":    []string{"1"},
		"sign": []string{"xyz"},
	})
	if raw != "a=1&b=2,3&" {
		t.Fatalf("raw = %q", raw)
	}
	if sign != "xyz" {
		t.Fatalf("sign = %q", sign)
	}
}

// TestMSVerifySignAcceptsFoldedHex 验证用参数内 sign 校验, 十六进制大小写不敏感.
func TestMSVerifySignAcceptsFoldedHex(t *testing.T) {
	key := "test.KEY-777"
	params := map[string]string{
		"a":    "1",
		"b":    "2",
		"sign": "",
	}
	raw, sign := MSGenSign(params, key)
	params["sign"] = strings.ToUpper(sign)
	gotRaw, match := MSVerifySign(params, key)
	if !match {
		t.Fatal("folded hex signature was rejected")
	}
	if gotRaw != raw {
		t.Fatalf("verify raw = %q, want %q", gotRaw, raw)
	}

	params["sign"] = "deadbeef"
	if _, match = MSVerifySign(params, key); match {
		t.Fatal("wrong signature was accepted")
	}
	if _, match = MSVerifySign(map[string]string{"a": "1"}, key); match {
		t.Fatal("missing sign field was accepted")
	}
}

// TestMSGetSignRawStructUsesJSONNumber 验证结构体走 JSON 再解 map, 整数保持 json.Number.
func TestMSGetSignRawStructUsesJSONNumber(t *testing.T) {
	type payload struct {
		A    string `json:"a"`
		T    int    `json:"t"`
		Skip string `json:"s"`
		Sign string `json:"sign"`
	}
	raw, sign := MSGetSignRaw(payload{A: "test", T: 1661834285, Skip: "", Sign: "ignore"})
	if raw != "a=test&t=1661834285&" {
		t.Fatalf("raw = %q", raw)
	}
	if sign != "ignore" {
		t.Fatalf("sign = %q", sign)
	}
}

// TestMSGenHMACSignVectors 用固定向量验证 HMAC-SHA256 签名, 拼串规则与 MSGenSign 相同.
//
// 向量与 crypto/hmac 和 Python hmac 独立计算结果一致; RFC 4231 用例 2 确认密钥与消息未颠倒.
func TestMSGenHMACSignVectors(t *testing.T) {
	if got := MSGenHMACSignWithRaw("what do ya want for nothing?", "Jefe"); got !=
		"5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Fatalf("RFC 4231 case 2 = %s", got)
	}
	raw, sign := MSGenHMACSign(map[string]any{
		"t": 1661834285, "a": "test", "z": true, "f": 3.14, "s": "", "nil": nil,
	}, "test.KEY-777")
	if raw != "a=test&f=3.14&t=1661834285&z=true&" {
		t.Fatalf("raw = %q", raw)
	}
	if sign != "a812e80f5b6666da29fdc83f04df3415081a78218d7758ddc65ca19c86b77e2c" {
		t.Fatalf("sign = %s", sign)
	}
	mac := hmac.New(sha256.New, []byte("test.KEY-777"))
	mac.Write([]byte(raw))
	if want := hex.EncodeToString(mac.Sum(nil)); sign != want {
		t.Fatalf("sign = %s, crypto/hmac = %s", sign, want)
	}
	// 请求签名场景: 方法, Key, 正文摘要与时间戳.
	_, sign = MSGenHMACSign(map[string]string{
		"body_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"key":         "game/cfg/main",
		"method":      "PUT",
		"ts":          "1792571400",
	}, "0123456789abcdef")
	if sign != "9a659762139c0924427cab51baebfc7b43498f5ce825023b759362407673a707" {
		t.Fatalf("request sign = %s", sign)
	}
	// 同一参数的 MD5 签名不受影响, 两种签名不能互换.
	if _, md5Sign := MSGenSign(map[string]string{"a": "1"}, "k"); len(md5Sign) != 32 {
		t.Fatalf("MD5 sign = %s", md5Sign)
	}
}

// TestMSVerifyHMACSign 覆盖往返校验, 大小写, 篡改以及各类非法 sign.
func TestMSVerifyHMACSign(t *testing.T) {
	const secret = "0123456789abcdef"
	params := map[string]string{"key": "game/cfg/main", "method": "PUT", "ts": "1792571400"}
	wantRaw, sign := MSGenHMACSign(params, secret)

	withSign := func(m map[string]string, s string) map[string]string {
		out := make(map[string]string, len(m)+1)
		for k, v := range m {
			out[k] = v
		}
		out[SignFieldName] = s
		return out
	}

	raw, match := MSVerifyHMACSign(withSign(params, sign), secret)
	if !match || raw != wantRaw {
		t.Fatalf("round trip: raw %q match %v", raw, match)
	}
	if _, match := MSVerifyHMACSign(withSign(params, strings.ToUpper(sign)), secret); !match {
		t.Fatal("uppercase sign rejected")
	}
	if _, match := MSVerifyHMACSign(withSign(params, sign), secret+"x"); match {
		t.Fatal("wrong secret accepted")
	}
	tampered := withSign(params, sign)
	tampered["method"] = "DELETE"
	if _, match := MSVerifyHMACSign(tampered, secret); match {
		t.Fatal("tampered params accepted")
	}
	for name, bad := range map[string]string{
		"non hex":      strings.Repeat("z", 64),
		"odd length":   sign[:63],
		"short":        sign[:32],
		"long":         sign + "00",
		"md5 sign":     MSGenSignWithRaw(wantRaw, secret),
		"with spaces":  " " + sign,
		"not the sign": strings.Repeat("0", 64),
	} {
		if _, match := MSVerifyHMACSign(withSign(params, bad), secret); match {
			t.Fatalf("%s accepted", name)
		}
	}
	if raw, match := MSVerifyHMACSign(params, secret); match || raw != wantRaw {
		t.Fatalf("missing sign: raw %q match %v", raw, match)
	}
	if _, match := MSVerifyHMACSign(nil, secret); match {
		t.Fatal("nil params accepted")
	}
}
