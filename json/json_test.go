package json

import (
	"bytes"
	"strings"
	"testing"
)

// TestRawMessageContract 验证 RawMessage 的 nil、复用和独立拷贝语义.
//
// UnmarshalJSON 必须复制调用方数据, Copy 返回值也不得与原切片共享底层数组;
// 这些边界会直接影响配置和跨项目消息对象的生命周期安全.
func TestRawMessageContract(t *testing.T) {
	var nilMessage RawMessage
	encoded, err := nilMessage.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal nil RawMessage: %v", err)
	}
	if string(encoded) != "null" {
		t.Fatalf("marshal nil RawMessage = %q, want null", encoded)
	}

	input := []byte(`{"value":1}`)
	message := RawMessage(`{"old":true}`)
	if err := message.UnmarshalJSON(input); err != nil {
		t.Fatalf("unmarshal RawMessage: %v", err)
	}
	input[2] = 'X'
	if string(message) != `{"value":1}` {
		t.Fatalf("RawMessage retained caller buffer: %s", message)
	}
	encoded, err = message.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal populated RawMessage: %v", err)
	}
	if string(encoded) != `{"value":1}` {
		t.Fatalf("marshal populated RawMessage = %s", encoded)
	}

	envelope, err := Marshal(struct {
		Raw RawMessage `json:"raw"`
	}{Raw: message})
	if err != nil {
		t.Fatalf("marshal RawMessage envelope: %v", err)
	}
	if string(envelope) != `{"raw":{"value":1}}` {
		t.Fatalf("marshal RawMessage envelope = %s", envelope)
	}

	copyMessage := message.Copy()
	copyMessage[2] = 'Y'
	if string(message) != `{"value":1}` {
		t.Fatalf("RawMessage.Copy shared backing storage: %s", message)
	}

	var nilTarget *RawMessage
	if err := nilTarget.UnmarshalJSON([]byte("null")); err == nil || !strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("nil RawMessage target error = %v", err)
	}
}

// TestCodecContract 验证两套 JSON 实现共同承诺的编码器和解码器行为.
func TestCodecContract(t *testing.T) {
	type payload struct {
		Name    string `json:"name"`
		Count   int    `json:"count"`
		Ignored string `json:"-"`
	}

	want := payload{Name: "pkg", Count: 2}
	encoded, err := Marshal(payload{Name: "pkg", Count: 2, Ignored: "secret"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if string(encoded) != `{"name":"pkg","count":2}` {
		t.Fatalf("marshal payload = %s", encoded)
	}

	var decoded payload
	if err := Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded != want {
		t.Fatalf("decoded payload = %+v, want %+v", decoded, want)
	}

	var stream bytes.Buffer
	encoder := NewEncoder(&stream)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(struct {
		HTML string `json:"html"`
	}{HTML: "<tag>&"}); err != nil {
		t.Fatalf("encode stream: %v", err)
	}
	if stream.String() != "{\"html\":\"<tag>&\"}\n" {
		t.Fatalf("encoded stream = %q", stream.String())
	}

	decoder := NewDecoder(strings.NewReader(`{"name":"stream","count":3}`))
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("decode stream: %v", err)
	}
	if decoded.Name != "stream" || decoded.Count != 3 {
		t.Fatalf("stream payload = %+v", decoded)
	}
}

// TestMustJSONHelpersContract 验证辅助函数的格式、HTML 转义和失败返回约定.
//
// 这些函数虽然以 Must 命名, 现有契约是在编码失败时返回 nil 而不是 panic;
// 测试只冻结既有行为, 避免在基础包中引入隐式破坏性变化.
func TestMustJSONHelpersContract(t *testing.T) {
	value := struct {
		HTML string `json:"html"`
	}{HTML: "<tag>&"}

	const escaped = `{"html":"\u003ctag\u003e\u0026"}`
	const escapedIndent = "{\n  \"html\": \"\\u003ctag\\u003e\\u0026\"\n}"
	const unescaped = `{"html":"<tag>&"}`
	const unescapedIndent = "{\n  \"html\": \"<tag>&\"\n}"

	if got := string(MustJSON(value)); got != escaped {
		t.Fatalf("MustJSON = %q", got)
	}
	if got := MustJSONString(value); got != escaped {
		t.Fatalf("MustJSONString = %q", got)
	}
	if got := string(MustJSONIndent(value)); got != escapedIndent {
		t.Fatalf("MustJSONIndent = %q", got)
	}
	if got := MustJSONIndentString(value); got != escapedIndent {
		t.Fatalf("MustJSONIndentString = %q", got)
	}
	if got := string(MustJSONUnEscape(value)); got != unescaped {
		t.Fatalf("MustJSONUnEscape = %q", got)
	}
	if got := MustJSONUnEscapeString(value); got != unescaped {
		t.Fatalf("MustJSONUnEscapeString = %q", got)
	}
	if got := string(MustJSONUnEscapeIndent(value)); got != unescapedIndent {
		t.Fatalf("MustJSONUnEscapeIndent = %q", got)
	}
	if got := MustJSONUnEscapeIndentString(value); got != unescapedIndent {
		t.Fatalf("MustJSONUnEscapeIndentString = %q", got)
	}

	unsupported := make(chan int)
	results := [][]byte{
		MustJSON(unsupported),
		MustJSONIndent(unsupported),
		MustJSONUnEscape(unsupported),
		MustJSONUnEscapeIndent(unsupported),
	}
	for i, result := range results {
		if result != nil {
			t.Fatalf("unsupported helper result[%d] = %q, want nil", i, result)
		}
	}
}
