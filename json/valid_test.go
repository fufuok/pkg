package json

import (
	"bytes"
	"strings"
	"testing"
)

// TestValidContract 验证 Valid 在默认 goccy 与 std_json 两个构建下结论一致.
//
// 本文件不带构建标签, CI 的普通测试与 -tags=std_json 测试各跑一遍同一张表.
// 表中只列两个后端结论相同的输入: 字符串内非法 UTF-8 和孤立代理项两者都判为合法,
// 需要严格 UTF-8 的调用方应另行调用 utf8.Valid, 这里固定该边界防止误用.
func TestValidContract(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "object", input: `{"a":1}`, want: true},
		{name: "array", input: `[1,"x",true,false,null]`, want: true},
		{name: "string", input: `"x"`, want: true},
		{name: "number", input: `-1.5e3`, want: true},
		{name: "true", input: `true`, want: true},
		{name: "null", input: `null`, want: true},
		{name: "big integer", input: `12345678901234567890`, want: true},
		{name: "big exponent", input: `1e400`, want: true},
		{name: "duplicate keys", input: `{"a":1,"a":2}`, want: true},
		{name: "surrounding whitespace", input: " \t\r\n{\"a\":1} \t\r\n", want: true},
		{name: "invalid utf8 in string", input: "\"\xff\xfe\"", want: true},
		{name: "lone surrogate escape", input: `"\ud800"`, want: true},
		{name: "empty", input: ``, want: false},
		{name: "whitespace only", input: " \n", want: false},
		{name: "two values", input: `{} {}`, want: false},
		{name: "two values without space", input: `{}{}`, want: false},
		{name: "trailing garbage", input: `null x`, want: false},
		{name: "bom", input: "\xef\xbb\xbf{}", want: false},
		{name: "form feed", input: "\f{}", want: false},
		{name: "trailing nul", input: "{}\x00", want: false},
		{name: "raw control char", input: "\"a\nb\"", want: false},
		{name: "leading zero", input: `01`, want: false},
		{name: "unclosed", input: `{"a":1`, want: false},
		{name: "trailing comma", input: `[1,]`, want: false},
	}
	for _, tc := range cases {
		if got := Valid([]byte(tc.input)); got != tc.want {
			t.Errorf("%s: Valid(%q) = %v, want %v (backend %s)", tc.name, tc.input, got, tc.want, Name)
		}
	}
}

// TestValidNestingLimit 固定两个后端共同的 10000 层嵌套上限.
//
// 调用方据此推算嵌入信封后的余量, 上限变化必须被测试发现.
func TestValidNestingLimit(t *testing.T) {
	for _, tc := range []struct {
		depth int
		want  bool
	}{
		{depth: 10000, want: true},
		{depth: 10001, want: false},
	} {
		array := strings.Repeat("[", tc.depth) + strings.Repeat("]", tc.depth)
		if got := Valid([]byte(array)); got != tc.want {
			t.Errorf("array depth %d: Valid = %v, want %v (backend %s)", tc.depth, got, tc.want, Name)
		}
		object := strings.Repeat(`{"a":`, tc.depth) + "1" + strings.Repeat("}", tc.depth)
		if got := Valid([]byte(object)); got != tc.want {
			t.Errorf("object depth %d: Valid = %v, want %v (backend %s)", tc.depth, got, tc.want, Name)
		}
	}
}

// TestValidLargePayloadContract 验证超过 4 MiB 的大正文在默认后端与 std_json 下保持完全一致的校验结果,
// 且超大合法正文稳态校验不产生整包拷贝分配.
func TestValidLargePayloadContract(t *testing.T) {
	item := []byte(`{"id":1,"msg":"` + strings.Repeat("x", 240) + `"},`)
	count := (4<<20)/len(item) + 64
	var buf bytes.Buffer
	buf.Grow(count*len(item) + 16)
	buf.WriteByte('[')
	for range count {
		buf.Write(item)
	}
	buf.WriteString(`{"id":2}]`)
	largeValid := buf.Bytes()
	if len(largeValid) <= 4<<20 {
		t.Fatalf("largeValid len = %d, want > 4 MiB", len(largeValid))
	}
	if !Valid(largeValid) {
		t.Fatalf("Valid(largeValid) = false, want true (backend %s)", Name)
	}
	if allocs := testing.AllocsPerRun(10, func() {
		if !Valid(largeValid) {
			t.Fatal("unexpected invalid result")
		}
	}); allocs != 0 {
		t.Fatalf("Valid(largeValid) allocs = %v, want 0 (backend %s)", allocs, Name)
	}

	largeInvalid := append(bytes.Clone(largeValid), 'x')
	if Valid(largeInvalid) {
		t.Fatalf("Valid(largeInvalid) = true, want false (backend %s)", Name)
	}
}
