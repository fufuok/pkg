package json

import (
	stdjson "encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

// TestJSONV2DefaultSemantics 冻结 Go 1.27 的关键事实:
// encoding/json 默认仍是 v1 语义, encoding/json/v2 才是新默认.
func TestJSONV2DefaultSemantics(t *testing.T) {
	var nilSlice []int
	v1, err := stdjson.Marshal(nilSlice)
	if err != nil {
		t.Fatalf("stdjson v1 marshal nil slice: %v", err)
	}
	if string(v1) != "null" {
		t.Fatalf("encoding/json nil slice = %s, want null (v1 semantics)", v1)
	}
	v2, err := jsonv2.Marshal(nilSlice)
	if err != nil {
		t.Fatalf("json v2 marshal nil slice: %v", err)
	}
	if string(v2) != "[]" {
		t.Fatalf("encoding/json/v2 nil slice = %s, want [] (v2 semantics)", v2)
	}

	html := "<tag>&"
	v1, err = stdjson.Marshal(html)
	if err != nil {
		t.Fatalf("stdjson v1 marshal html: %v", err)
	}
	if string(v1) != `"\u003ctag\u003e\u0026"` {
		t.Fatalf("encoding/json html = %s, want escaped v1 output", v1)
	}
	v2, err = jsonv2.Marshal(html)
	if err != nil {
		t.Fatalf("json v2 marshal html: %v", err)
	}
	if string(v2) != `"<tag>&"` {
		t.Fatalf("encoding/json/v2 html = %s, want minimal v2 output", v2)
	}
}
