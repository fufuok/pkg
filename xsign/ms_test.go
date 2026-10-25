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

// TestMSGenHMACSignVectors 用固定向量验证 HMAC-SHA256 和规范表单编码.
//
// 向量与 crypto/hmac 独立计算结果一致; RFC 4231 用例 2 确认密钥与消息未颠倒.
func TestMSGenHMACSignVectors(t *testing.T) {
	if got := MSGenHMACSignWithRaw("what do ya want for nothing?", "Jefe"); got !=
		"5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Fatalf("RFC 4231 case 2 = %s", got)
	}
	raw, sign := MSGenHMACSign(url.Values{
		"t": {"1661834285"}, "a": {"test"}, "z": {"true"}, "f": {"3.14"}, "s": {""},
	}, "test.KEY-777")
	if raw != "a=test&f=3.14&s=&t=1661834285&z=true" {
		t.Fatalf("raw = %q", raw)
	}
	mac := hmac.New(sha256.New, []byte("test.KEY-777"))
	mac.Write([]byte(raw))
	if want := hex.EncodeToString(mac.Sum(nil)); sign != want {
		t.Fatalf("sign = %s, crypto/hmac = %s", sign, want)
	}
	// 请求签名场景: 方法, Key, 正文摘要与时间戳.
	raw, sign = MSGenHMACSign(url.Values{
		"body_sha256": {"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		"key":         {"game/cfg/main"},
		"method":      {"PUT"},
		"ts":          {"1792571400"},
	}, "0123456789abcdef")
	if raw != "body_sha256=e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855&key=game%2Fcfg%2Fmain&method=PUT&ts=1792571400" {
		t.Fatalf("request raw = %q", raw)
	}
	if sign != MSGenHMACSignWithRaw(raw, "0123456789abcdef") {
		t.Fatalf("request sign = %s did not match raw signature", sign)
	}
	// 同一参数的 MD5 签名不受影响, 两种签名不能互换.
	if _, md5Sign := MSGenSign(map[string]string{"a": "1"}, "k"); len(md5Sign) != 32 {
		t.Fatalf("MD5 sign = %s", md5Sign)
	}
}

// TestMSVerifyHMACSign 覆盖规范编码往返校验, 大小写, 篡改及非法 sign.
func TestMSVerifyHMACSign(t *testing.T) {
	const secret = "0123456789abcdef"
	params := url.Values{"key": {"game/cfg/main"}, "method": {"PUT"}, "ts": {"1792571400"}}
	wantRaw, sign := MSGenHMACSign(params, secret)

	raw, match := MSVerifyHMACSign(params, sign, secret)
	if !match || raw != wantRaw {
		t.Fatalf("round trip: raw %q match %v", raw, match)
	}
	if _, match := MSVerifyHMACSign(params, strings.ToUpper(sign), secret); !match {
		t.Fatal("uppercase sign rejected")
	}
	if _, match := MSVerifyHMACSign(params, sign, secret+"x"); match {
		t.Fatal("wrong secret accepted")
	}
	tampered := url.Values{"key": {"game/cfg/main"}, "method": {"PUT"}, "ts": {"1792571400"}}
	tampered.Set("method", "DELETE")
	if _, match := MSVerifyHMACSign(tampered, sign, secret); match {
		t.Fatal("tampered params accepted")
	}
	for name, bad := range map[string]string{
		"missing":     "",
		"non hex":     strings.Repeat("z", 64),
		"odd length":  sign[:63],
		"short":       sign[:32],
		"long":        sign + "00",
		"md5 sign":    MSGenSignWithRaw(wantRaw, secret),
		"with spaces": " " + sign,
		"wrong sign":  strings.Repeat("0", 64),
	} {
		if _, match := MSVerifyHMACSign(params, bad, secret); match {
			t.Fatalf("%s accepted", name)
		}
	}
	if raw, _ := MSVerifyHMACSign(params, "invalid", secret); raw != "" {
		t.Fatalf("invalid format returned raw %q, want empty", raw)
	}
}

// TestMSHMACSignSeparatesFormValues 验证新 HMAC 规范化保留字段边界, 空值和多值,
// 并禁止把一组参数的有效签名复用在结构不同的另一组参数上.
func TestMSHMACSignSeparatesFormValues(t *testing.T) {
	const secret = "review-only-secret"
	cases := []struct {
		name string
		a, b url.Values
	}{
		{"value delimiter", url.Values{"a": {"x&b=y"}}, url.Values{"a": {"x"}, "b": {"y"}}},
		{"key delimiter", url.Values{"a=b": {"c"}}, url.Values{"a": {"b=c"}}},
		{"multiple values", url.Values{"a": {"x,y"}}, url.Values{"a": {"x", "y"}}},
		{"missing versus empty", url.Values{}, url.Values{"a": {""}}},
		{"literal percent", url.Values{"a": {"x&b=y"}}, url.Values{"a": {"x%26b%3Dy"}}},
		{"sign is ordinary data", url.Values{"sign": {"one"}}, url.Values{"sign": {"two"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rawA, signA := MSGenHMACSign(tc.a, secret)
			rawB, signB := MSGenHMACSign(tc.b, secret)
			if rawA == rawB || signA == signB {
				t.Fatalf("different parameters collided: %q and %q", rawA, rawB)
			}
			if _, match := MSVerifyHMACSign(tc.b, signA, secret); match {
				t.Fatal("signature was accepted for different parameters")
			}
		})
	}
}
