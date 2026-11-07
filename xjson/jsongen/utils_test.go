package jsongen

import (
	"testing"

	"github.com/fufuok/pkg/pools/bytespool"
	"github.com/fufuok/pkg/utils"
)

func TestAppendJSONString(t *testing.T) {
	bs := AppendJSONString([]byte("[1,2,"), "A\fB")
	bs = append(bs, "]"...)
	if string(bs) != `[1,2,"A\fB"]` {
		t.Fatalf(`[1,2,"A\fB"] != %s`, bs)
	}
	bs = AppendJSONString(nil, "A\nB")
	if string(bs) != `"A\nB"` {
		t.Fatalf(`"A\nB" != %s`, bs)
	}
	s := "A\rB"
	dst := bytespool.Make(len(s) + 2)
	ss := utils.B2S(AppendJSONString(dst, s))
	if ss != `"A\rB"` {
		t.Fatalf(`"A\rB" != %s`, ss)
	}
}

// TestJSONEscapeSpecialAndOverflowEdges 覆盖控制字符、HTML 开关、非法 UTF-8、U+FFFD、行分隔符及超 16 键溢出.
func TestJSONEscapeSpecialAndOverflowEdges(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", `""`},
		{"plain_ascii-123", `"plain_ascii-123"`},
		{"中国|四川|成都|电信", `"中国|四川|成都|电信"`},
		{"a<b&c>d", `"a\u003cb\u0026c\u003ed"`},
		{"line\u2028sep\u2029end", `"line\u2028sep\u2029end"`},
		{"valid-replacement-\ufffd", "\"valid-replacement-\ufffd\""},
		{"invalid-byte-\xff-tail", `"invalid-byte-\ufffd-tail"`},
		{"ctrl-\x00-\b-\t-\n-\f-\r-\x1f-\x7f", "\"ctrl-\\u0000-\\b-\\t-\\n-\\f-\\r-\\u001f-\x7f\""},
	}
	for _, tc := range cases {
		if got := EscapeString(tc.in); got != tc.want {
			t.Fatalf("EscapeString(%q) = %q, want %q", tc.in, got, tc.want)
		}
		m := NewMap()
		m.PutString(tc.in, tc.in)
		raw := m.Serialize(nil)
		if len(raw) != m.Size() {
			t.Fatalf("Size mismatch for %q: len=%d, Size=%d", tc.in, len(raw), m.Size())
		}
		wantObj := "{" + tc.want + ":" + tc.want + "}"
		if string(raw) != wantObj {
			t.Fatalf("Map.Serialize(%q) = %s, want %s", tc.in, raw, wantObj)
		}
	}

	// 验证超过 16 个内联字段溢出到 extra 切片及极值整数精度与 Size 一致.
	big := NewMap()
	for i := range 24 {
		big.PutInt("k_"+string(rune('a'+i)), int64(i)*-123456789)
	}
	big.PutInt("min_int64", -9223372036854775808)
	big.PutUint("max_uint64", 18446744073709551615)
	out := big.Serialize(nil)
	if len(out) != big.Size() {
		t.Fatalf("overflow Map Size = %d, len(Serialize) = %d", big.Size(), len(out))
	}
}
