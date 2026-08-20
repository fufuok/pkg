package xsign

import (
	"net/url"
	"strings"
	"testing"
)

const testToken = "test.KEY-777"

// TestGenSignMatchesMicroserviceGolden 对齐 DataPlugins/DNSCollect 的拼串 MD5 金值.
// 空值、非对象和忽略字段都必须得到同一摘要, 不能改成 HMAC 或 JSON 规范化.
func TestGenSignMatchesMicroserviceGolden(t *testing.T) {
	tests := []struct {
		name  string
		param any
		want  string
	}{
		{name: "nil", param: nil, want: "df1f04e971d6ce284fa372fa81652e9e"},
		{name: "empty-struct", param: struct{}{}, want: "df1f04e971d6ce284fa372fa81652e9e"},
		{name: "empty-string", param: "", want: "df1f04e971d6ce284fa372fa81652e9e"},
		{name: "plain-string", param: "123", want: "df1f04e971d6ce284fa372fa81652e9e"},
		{name: "plain-int", param: 456, want: "df1f04e971d6ce284fa372fa81652e9e"},
		{
			name: "map-any",
			param: map[string]any{
				"t":   1661834285,
				"a":   "test",
				"z":   true,
				"f":   3.14,
				"s":   "",
				"nil": nil,
			},
			want: "bc9d8821a9df04b663aaba90750eaa15",
		},
		{
			name: "map-string",
			param: map[string]string{
				"zh":   "中　 文",
				"u":    "\\u3000",
				"sign": "ignore",
			},
			want: "64fdc52800ad3de0bef8e36d69a031f2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := GenSign(tt.param, testToken)
			if got != tt.want {
				t.Fatalf("sign = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestGetSignRawSkipsEmptyAndSign 验证空值不进拼串, sign 字段只抽出不参与摘要.
func TestGetSignRawSkipsEmptyAndSign(t *testing.T) {
	raw, sign := GetSignRaw(map[string]string{
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

// TestGetSignRawURLValuesJoinsWithComma 验证 url.Values 多值用逗号拼接后再按 key 排序.
func TestGetSignRawURLValuesJoinsWithComma(t *testing.T) {
	raw, sign := GetSignRaw(url.Values{
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

// TestVerifySignAcceptsFoldedHex 验证用参数内 sign 校验, 十六进制大小写不敏感.
func TestVerifySignAcceptsFoldedHex(t *testing.T) {
	params := map[string]string{
		"a":    "1",
		"b":    "2",
		"sign": "",
	}
	raw, sign := GenSign(params, testToken)
	params["sign"] = strings.ToUpper(sign)
	gotRaw, match := VerifySign(params, testToken)
	if !match {
		t.Fatal("folded hex signature was rejected")
	}
	if gotRaw != raw {
		t.Fatalf("verify raw = %q, want %q", gotRaw, raw)
	}

	params["sign"] = "deadbeef"
	if _, match = VerifySign(params, testToken); match {
		t.Fatal("wrong signature was accepted")
	}
	if _, match = VerifySign(map[string]string{"a": "1"}, testToken); match {
		t.Fatal("missing sign field was accepted")
	}
}

// TestGetSignRawStructUsesJSONNumber 验证结构体走 JSON 再解 map, 整数保持 json.Number.
func TestGetSignRawStructUsesJSONNumber(t *testing.T) {
	type payload struct {
		A    string `json:"a"`
		T    int    `json:"t"`
		Skip string `json:"s"`
		Sign string `json:"sign"`
	}
	raw, sign := GetSignRaw(payload{A: "test", T: 1661834285, Skip: "", Sign: "ignore"})
	if raw != "a=test&t=1661834285&" {
		t.Fatalf("raw = %q", raw)
	}
	if sign != "ignore" {
		t.Fatalf("sign = %q", sign)
	}
}
