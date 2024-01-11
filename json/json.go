package json

import (
	"errors"
	"unsafe"
)

type RawMessage []byte

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
	tmp := make([]byte, len(m))
	copy(tmp, m)
	return tmp
}

// MustJSONIndent 转 json 返回 []byte
func MustJSONIndent(v any) []byte {
	js, _ := MarshalIndent(v, "", "  ")
	return js
}

// MustJSONIndentString 转 json 返回 string
func MustJSONIndentString(v any) string {
	js := MustJSONIndent(v)
	return *(*string)(unsafe.Pointer(&js))
}

// MustJSON 转 json 返回 []byte
func MustJSON(v any) []byte {
	js, _ := Marshal(v)
	return js
}

// MustJSONString 转 json 返回 string
func MustJSONString(v any) string {
	js := MustJSON(v)
	return *(*string)(unsafe.Pointer(&js))
}
