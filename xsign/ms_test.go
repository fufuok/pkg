package xsign

import (
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
