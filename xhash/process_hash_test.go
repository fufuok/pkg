package xhash

import (
	"bytes"
	"strings"
	"testing"
)

// TestMemHashViews 验证哈希只由内容决定, 不受 nil, 容量, 起点和字符串底层地址影响.
// 分别比较 32/64 位接口, 不假定两种 seed 的结果可以相互截断.
func TestMemHashViews(t *testing.T) {
	t.Parallel()
	backing := []byte("prefix-value-suffix")
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil"},
		{name: "empty", data: []byte{}},
		{name: "empty with capacity", data: backing[:0]},
		{name: "subslice", data: backing[7:12]},
		{name: "binary", data: []byte{0, 0xff, 0, 0x80}},
		{name: "long", data: bytes.Repeat([]byte("中文\x00"), 1024)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := string(tt.data)
			// 构造独立分配内的子字符串, 同时覆盖非零起点和内容边界.
			view := strings.Clone("[" + content + "]")[1 : len(content)+1]
			want64, want32 := MemHash(content), MemHash32(content)
			if got := MemHashb(tt.data); got != want64 {
				t.Fatalf("byte hash = %d, want string hash %d", got, want64)
			}
			if got := MemHashb32(tt.data); got != want32 {
				t.Fatalf("32-bit byte hash = %d, want string hash %d", got, want32)
			}
			if MemHash(view) != want64 || MemHash32(view) != want32 {
				t.Fatal("substring hash depends on the backing address or surrounding bytes")
			}
		})
	}
}
