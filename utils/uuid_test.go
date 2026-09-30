package utils

import (
	"bytes"
	"encoding/hex"
	"testing"
	"uuid"

	"github.com/fufuok/pkg/base58"
	"github.com/fufuok/pkg/xid"
)

// TestUUID 验证二进制 UUID 的版本和变体, 不以概率性的碰撞检查判断随机质量.
func TestUUID(t *testing.T) {
	t.Parallel()
	id := UUID()
	checkUUIDV4(t, id)

	// 返回切片由调用方拥有, 修改另一份 UUID 不得影响已有结果.
	want := bytes.Clone(id)
	other := UUID()
	copy(other, id)
	other[0] ^= 0xff
	if !bytes.Equal(id, want) {
		t.Fatal("UUID calls share mutable storage")
	}
}

// TestUUIDString 验证标准短横线形式和小写约定, 同时检查实际返回值的版本和变体.
func TestUUIDString(t *testing.T) {
	t.Parallel()
	s := UUIDString()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse UUIDString: %v", err)
	}
	if s != id.String() {
		t.Fatalf("UUIDString is not canonical lowercase text: %q", s)
	}
	checkUUIDV4(t, id[:])
}

// TestUUIDSimple 验证无短横线的 32 位小写十六进制格式.
func TestUUIDSimple(t *testing.T) {
	t.Parallel()
	s := UUIDSimple()
	id, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode UUIDSimple: %v", err)
	}
	if s != hex.EncodeToString(id) {
		t.Fatalf("UUIDSimple is not lowercase hexadecimal: %q", s)
	}
	checkUUIDV4(t, id)
}

// TestUUIDShort 验证 base58 编码保留完整 UUID, 不假定随机文本具有固定长度.
func TestUUIDShort(t *testing.T) {
	t.Parallel()
	s := UUIDShort()
	id := base58.Decode(s)
	checkUUIDV4(t, id)
	if s != base58.Encode(id) {
		t.Fatalf("UUIDShort is not canonical base58 text: %q", s)
	}
}

// TestEncodeUUID 固定任意长度输入的编码契约, 包括补零、截断和不校验版本位.
func TestEncodeUUID(t *testing.T) {
	t.Parallel()
	full := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	tests := []struct {
		name string
		id   []byte
		want string
	}{
		{name: "nil", want: "00000000-0000-0000-0000-000000000000"},
		{name: "empty", id: []byte{}, want: "00000000-0000-0000-0000-000000000000"},
		{name: "short", id: []byte{0xab, 0xcd, 0xef}, want: "abcdef00-0000-0000-0000-000000000000"},
		{name: "full", id: full, want: "00112233-4455-6677-8899-aabbccddeeff"},
		{name: "long", id: append(bytes.Clone(full), 0x12, 0x34), want: "00112233-4455-6677-8899-aabbccddeeff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			before := bytes.Clone(tt.id)
			got := EncodeUUID(tt.id)
			if string(got) != tt.want {
				t.Fatalf("EncodeUUID = %q, want %q", got, tt.want)
			}
			if !bytes.Equal(tt.id, before) {
				t.Fatal("EncodeUUID modified its input")
			}
		})
	}
}

// checkUUIDV4 检查生成结果的长度和固定标志位, 不限制随机数据的具体取值.
func checkUUIDV4(t *testing.T, id []byte) {
	t.Helper()
	if len(id) != 16 {
		t.Fatalf("UUID length = %d, want 16", len(id))
	}
	if id[6]>>4 != 4 {
		t.Fatalf("UUID version = %d, want 4", id[6]>>4)
	}
	if id[8]&0xc0 != 0x80 {
		t.Fatalf("UUID variant bits = %#x, want 0x80", id[8]&0xc0)
	}
}

func BenchmarkUniqueUUID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = UUID()
	}
}

func BenchmarkUniqueUUIDString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = UUIDString()
	}
}

func BenchmarkUniqueUUIDSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = UUIDSimple()
	}
}

func BenchmarkUniqueXID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = xid.NewBytes()
	}
}

func BenchmarkUniqueXIDString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = xid.NewString()
	}
}

// BenchmarkUniqueUUID-8         	 1516308	      2409 ns/op	      16 B/op	       1 allocs/op
// BenchmarkUniqueUUID-8         	 1428595	      3121 ns/op	      16 B/op	       1 allocs/op
// BenchmarkUniqueUUID-8         	 1454421	      2641 ns/op	      16 B/op	       1 allocs/op
// BenchmarkUniqueUUIDString-8   	 1320424	      2716 ns/op	      64 B/op	       2 allocs/op
// BenchmarkUniqueUUIDString-8   	 1400548	      2770 ns/op	      64 B/op	       2 allocs/op
// BenchmarkUniqueUUIDString-8   	 1000000	      3083 ns/op	      64 B/op	       2 allocs/op
// BenchmarkUniqueUUIDSimple-8   	 1000000	      3235 ns/op	      80 B/op	       3 allocs/op
// BenchmarkUniqueUUIDSimple-8   	 1202796	      2658 ns/op	      80 B/op	       3 allocs/op
// BenchmarkUniqueUUIDSimple-8   	 1317488	      2608 ns/op	      80 B/op	       3 allocs/op
// BenchmarkUniqueXID-8          	 2616777	      1411 ns/op	       0 B/op	       0 allocs/op
// BenchmarkUniqueXID-8          	 2530496	      1378 ns/op	       0 B/op	       0 allocs/op
// BenchmarkUniqueXID-8          	 2729223	      1389 ns/op	       0 B/op	       0 allocs/op
// BenchmarkUniqueXIDString-8    	 2332801	      1525 ns/op	      32 B/op	       1 allocs/op
// BenchmarkUniqueXIDString-8    	 2385540	      1611 ns/op	      32 B/op	       1 allocs/op
// BenchmarkUniqueXIDString-8    	 2197720	      1516 ns/op	      32 B/op	       1 allocs/op
