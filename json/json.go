package json

import (
	"bytes"
	"errors"

	"github.com/fufuok/bytespool"
	"github.com/fufuok/utils"
)

type RawMessage []byte // nolint: recvcheck

// MarshalJSON returns m as the JSON encoding of m.
func (m RawMessage) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	return m, nil
}

// UnmarshalJSON sets *m to a copy of data.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	if m == nil {
		return errors.New("json.RawMessage: UnmarshalJSON on nil pointer")
	}
	*m = append((*m)[0:0], data...)
	return nil
}

// Copy returns a copy of m.
func (m RawMessage) Copy() []byte {
	return bytespool.NewBytes(m)
}

// MustJSONIndent 转 json 返回 []byte
func MustJSONIndent(v any) []byte {
	js, _ := MarshalIndent(v, "", "  ")
	return js
}

// MustJSONIndentString 转 json 返回 string
func MustJSONIndentString(v any) string {
	js := MustJSONIndent(v)
	return utils.B2S(js)
}

// MustJSON 转 json 返回 []byte
func MustJSON(v any) []byte {
	js, _ := Marshal(v)
	return js
}

// MustJSONString 转 json 返回 string
func MustJSONString(v any) string {
	js := MustJSON(v)
	return utils.B2S(js)
}

// MustJSONUnEscapeIndent 转 json 返回 []byte, 不转义 ( '&', '<', '>' )
func MustJSONUnEscapeIndent(v any) []byte {
	buf := bytes.NewBuffer(nil)
	enc := NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return buf.Bytes()[:buf.Len()-1]
}

// MustJSONUnEscapeIndentString 转 json 返回 string, 不转义 ( '&', '<', '>' )
func MustJSONUnEscapeIndentString(v any) string {
	js := MustJSONUnEscapeIndent(v)
	return utils.B2S(js)
}

// MustJSONUnEscape 转 json 返回 []byte, 不转义 ( '&', '<', '>' )
func MustJSONUnEscape(v any) []byte {
	buf := bytes.NewBuffer(nil)
	enc := NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil
	}
	return buf.Bytes()[:buf.Len()-1]
}

// MustJSONUnEscapeString 转 json 返回 string, 不转义 ( '&', '<', '>' )
func MustJSONUnEscapeString(v any) string {
	js := MustJSONUnEscape(v)
	return utils.B2S(js)
}
