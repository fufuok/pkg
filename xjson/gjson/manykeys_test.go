package gjson

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// mkKeyPool 覆盖普通键、路径特殊字符键、转义字符键、通配符键、数字键和空键.
var mkKeyPool = []string{
	"brand", "x_event_id", "_ctime", "_x", "type", "data", "dup", "a", "a.b", "a|b", "#", "@this", "!true",
	"[a]", "{a}", "a:b", "中文", "é", `q"k`, `b\s`, "-1", "0", "..", " sp", "a*", "?", "",
}

// mkSame 逐字段比较两个结果; Num 按位比较 (区分 +0/-0 与不同的 NaN 位模式), Indexes 按元素比较.
func mkSame(a, b Result) bool {
	return a.Type == b.Type && a.Raw == b.Raw && a.Str == b.Str && a.Index == b.Index &&
		math.Float64bits(a.Num) == math.Float64bits(b.Num) && slices.Equal(a.Indexes, b.Indexes)
}

// mkCheck 以 Get(doc, Escape(k)) 为对照逐键核对 GetManyKeys, 返回不一致的数量, 只报告前几个差异.
func mkCheck(t testing.TB, doc string, keys []string, dst []Result) int {
	t.Helper()
	got := GetManyKeys(doc, keys, dst)
	if len(got) != len(keys) {
		t.Fatalf("len(got)=%d, want %d", len(got), len(keys))
	}
	bad := 0
	for i, k := range keys {
		want := Get(doc, Escape(k))
		if !mkSame(got[i], want) {
			bad++
			if bad <= 3 {
				t.Errorf("key %q doc=%q\ngot =%#v\nwant=%#v", k, doc, got[i], want)
			}
		}
	}
	return bad
}

// mkValue 生成随机 JSON 值: 普通/转义字符串、整数、浮点、字面量、嵌套对象和数组, 嵌套深度受限.
func mkValue(rng *rand.Rand, depth int) string {
	k := rng.IntN(12)
	if depth > 2 && k >= 9 {
		k = rng.IntN(9)
	}
	switch k {
	case 0, 1:
		return strconv.Quote([]string{"track", "nh_ping", "xy", "TCP", "多线", "api.example.com", ""}[rng.IntN(7)])
	case 2:
		parts := []string{"a", `\"`, `\\`, `\n`, `\u00e9`, `\ud83d\ude00`, `\ud83d`, "中", " ", "/", `\/`}
		var b strings.Builder
		b.WriteByte('"')
		for range rng.IntN(5) {
			b.WriteString(parts[rng.IntN(len(parts))])
		}
		b.WriteByte('"')
		return b.String()
	case 3:
		return strconv.Itoa(rng.IntN(2_000_000_000) - 1000)
	case 4:
		return []string{"0", "-1", "9007199254740993", "18446744073709551615", "1e3", "-0", "1.5", "NaN", "-Inf"}[rng.IntN(9)]
	case 5:
		return strconv.FormatFloat(rng.Float64()*1000, 'f', -1, 64)
	case 6:
		return []string{"true", "false", "null"}[rng.IntN(3)]
	case 7, 8:
		return `"2026-11-13T00:06:56Z"`
	case 9, 10:
		return mkObject(rng, depth+1, 1+rng.IntN(6))
	default:
		var b strings.Builder
		b.WriteString("[ ")
		for i := range rng.IntN(6) {
			if i > 0 {
				b.WriteString(" , ")
			}
			b.WriteString(mkValue(rng, depth+1))
		}
		b.WriteString(" ]")
		return b.String()
	}
}

// mkKeyJSON 从键池取一个键并编码为 JSON 键; 约 1/6 概率整体写成 \uXXXX 转义形式, 检验反转义后比较.
func mkKeyJSON(rng *rand.Rand) string {
	k := mkKeyPool[rng.IntN(len(mkKeyPool))]
	if rng.IntN(6) != 0 {
		return strconv.Quote(k)
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range k {
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	b.WriteByte('"')
	return b.String()
}

// mkObject 生成含 n 个随机键值对的对象, 分隔符随机夹带空白, 键可能重复.
func mkObject(rng *rand.Rand, depth, n int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := range n {
		if i > 0 {
			b.WriteString([]string{",", " , ", ",\n"}[rng.IntN(3)])
		}
		b.WriteString(mkKeyJSON(rng))
		b.WriteString([]string{":", " : "}[rng.IntN(2)])
		b.WriteString(mkValue(rng, depth))
	}
	b.WriteByte('}')
	return b.String()
}

// mkMutate 随机破坏 JSON: 截断、删字节、插入结构字符或改写任意字节, 用于覆盖畸形输入.
func mkMutate(rng *rand.Rand, s string) string {
	b := []byte(s)
	for range 1 + rng.IntN(3) {
		if len(b) == 0 {
			break
		}
		p := rng.IntN(len(b))
		switch rng.IntN(4) {
		case 0:
			b = b[:p]
		case 1:
			b = append(b[:p], b[p+1:]...)
		case 2:
			c := []byte(`{}[]",:\ `)[rng.IntN(9)]
			b = append(b[:p], append([]byte{c}, b[p:]...)...)
		default:
			b[p] = byte(rng.IntN(256))
		}
	}
	return string(b)
}

// TestGetManyKeysOracle 随机对象及其畸形变体与逐键 Get(doc, Escape(k)) 对比, 要求逐字段完全一致.
func TestGetManyKeysOracle(t *testing.T) {
	iterations := 200000
	if testing.Short() {
		iterations = 20000
	}
	rng := rand.New(rand.NewPCG(20261113, 1))
	var dst [8]Result
	total, bad := 0, 0
	for it := range iterations {
		var b strings.Builder
		if rng.IntN(10) == 0 {
			// 前导字节: Get 会跳过任意字节直到第一个 '{' 或 '['.
			b.WriteString([]string{" ", "xx", "[", "\n\t", `"`}[rng.IntN(5)])
		}
		b.WriteString(mkObject(rng, 1, rng.IntN(12)))
		doc := b.String()
		if it%3 == 0 {
			doc = mkMutate(rng, doc)
		}
		keys := make([]string, 1+rng.IntN(5))
		for i := range keys {
			keys[i] = mkKeyPool[rng.IntN(len(mkKeyPool))]
		}
		if rng.IntN(8) == 0 {
			keys = append(keys, keys[0]) // 请求内重复键
		}
		total += len(keys)
		// 交替使用调用方数组和 nil, 覆盖复用与新分配两条路径.
		if it%2 == 0 {
			bad += mkCheck(t, doc, keys, dst[:0])
		} else {
			bad += mkCheck(t, doc, keys, nil)
		}
		if bad > 10 {
			t.Fatalf("too many mismatches, stop after %d iterations", it)
		}
	}
	t.Logf("compared=%d mismatches=%d", total, bad)
}

// TestGetManyKeysCases 覆盖文档化的语义边界, 每个用例都以逐键 Get 为对照, 并核对关键值.
func TestGetManyKeysCases(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		keys []string
		want []string // 期望的 Raw, 空串表示不存在
	}{
		{"duplicate json key keeps first", `{"a":1,"b":2,"a":3}`, []string{"a", "b"}, []string{"1", "2"}},
		{"duplicate request keys share result", `{"a":"x","b":2}`, []string{"a", "b", "a"}, []string{`"x"`, "2", `"x"`}},
		{"nested key is not top level", `{"b":{"a":9},"a":1}`, []string{"a"}, []string{"1"}},
		{"dotted key is literal", `{"a":{"b":1},"a.b":2}`, []string{"a.b"}, []string{"2"}},
		{"escaped json key", `{"\u0061":5,"b\u0000":7,"b":6}`, []string{"a", "b"}, []string{"5", "6"}},
		{"escaped value", `{"a":"x\"yé"}`, []string{"a"}, []string{`"x\"yé"`}},
		{
			"all types", `{"s":"v","n":-1.5,"t":true,"f":false,"z":null,"o":{"x":[1]},"r":[1,{"y":2}]}`,
			[]string{"s", "n", "t", "f", "z", "o", "r"},
			[]string{`"v"`, "-1.5", "true", "false", "null", `{"x":[1]}`, `[1,{"y":2}]`},
		},
		{"missing key scans to end", `{"a":1,"b":2}`, []string{"a", "c"}, []string{"1", ""}},
		{"leading garbage", `xx {"a":1}`, []string{"a"}, []string{"1"}},
		{"truncated object", `{"a":1,"b":`, []string{"a", "b"}, []string{"1", ""}},
		{"stop at top level end", `{"a":1}{"b":2}`, []string{"a", "b"}, []string{"1", ""}},
		{"top level array falls back", `[{"a":1},2]`, []string{"0", "1"}, []string{`{"a":1}`, "2"}},
		{"wildcard key falls back", `{"a*b":1,"axb":2}`, []string{"a*b", "axb"}, []string{"1", "2"}},
		{"empty key falls back", `{"":1,"a":2}`, []string{"", "a"}, []string{"1", "2"}},
		{
			"path chars are literal", `{"@this":1,"a|b":2,"#":3,"!true":4}`,
			[]string{"@this", "a|b", "#", "!true"},
			[]string{"1", "2", "3", "4"},
		},
		{"no container", `"a"`, []string{"a"}, []string{""}},
		{"empty input", ``, []string{"a", "b"}, []string{"", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if bad := mkCheck(t, c.doc, c.keys, nil); bad != 0 {
				t.Fatalf("%d mismatches against Get", bad)
			}
			got := GetManyKeys(c.doc, c.keys, nil)
			for i := range c.keys {
				if got[i].Raw != c.want[i] {
					t.Fatalf("key %q Raw=%q, want %q", c.keys[i], got[i].Raw, c.want[i])
				}
			}
		})
	}
}

// TestGetManyKeysKeyCountLimit 64 个键走单遍扫描, 65 个键逐键回退, 两者结果都与 Get 一致.
func TestGetManyKeysKeyCountLimit(t *testing.T) {
	var b strings.Builder
	b.WriteByte('{')
	keys := make([]string, 0, manyKeysMax+1)
	for i := range manyKeysMax + 1 {
		if i > 0 {
			b.WriteByte(',')
		}
		k := "k" + strconv.Itoa(i)
		fmt.Fprintf(&b, "%q:%d", k, i)
		keys = append(keys, k)
	}
	b.WriteByte('}')
	doc := b.String()
	for _, n := range []int{1, manyKeysMax - 1, manyKeysMax, manyKeysMax + 1} {
		if bad := mkCheck(t, doc, keys[:n], nil); bad != 0 {
			t.Fatalf("n=%d: %d mismatches", n, bad)
		}
		got := GetManyKeys(doc, keys[:n], nil)
		if got[n-1].Int() != int64(n-1) {
			t.Fatalf("n=%d: last value %v, want %d", n, got[n-1].Int(), n-1)
		}
	}
}

// TestGetManyKeysDst 覆盖 dst 的复用、清零和扩容: 复用时返回同一底层数组, 旧结果不能残留.
func TestGetManyKeysDst(t *testing.T) {
	var buf [4]Result
	got := GetManyKeys(`{"a":1,"b":2}`, []string{"a", "b"}, buf[:0])
	if &got[0] != &buf[0] || got[1].Int() != 2 {
		t.Fatalf("expect reuse of dst, got %#v", got)
	}
	// 第二次查询不存在的键: 上一次留在 buf 中的结果必须被清零.
	got = GetManyKeys(`{"x":1}`, []string{"a", "b"}, buf[:2])
	if got[0].Exists() || got[1].Exists() {
		t.Fatalf("stale result leaked: %#v", got)
	}
	// 容量不足时新分配, 不越界写入调用方数组.
	small := buf[:0:1]
	got = GetManyKeys(`{"a":1,"b":2}`, []string{"a", "b"}, small)
	if len(got) != 2 || &got[0] == &buf[0] || got[1].Int() != 2 {
		t.Fatalf("expect new allocation, got %#v", got)
	}
	if got := GetManyKeys(`{"a":1}`, nil, nil); len(got) != 0 {
		t.Fatalf("empty keys: got %#v", got)
	}
}

// TestGetManyKeysBorrow 结果引用输入内存: 输入是可变字节的 unsafe 视图时, 改写输入会改变结果.
// 用于固定 "结果借用 json" 的文档约定, 防止调用方误以为结果是独立副本.
func TestGetManyKeysBorrow(t *testing.T) {
	b := []byte(`{"a":"xy","b":12}`)
	doc := bytesString(b)
	got := GetManyKeys(doc, []string{"a", "b"}, nil)
	if got[0].Str != "xy" || got[1].Raw != "12" {
		t.Fatalf("unexpected result %#v", got)
	}
	copy(b[6:8], "zz")
	if got[0].Str != "zz" {
		t.Fatalf("expect borrowed Str to follow input bytes, got %q", got[0].Str)
	}
}

// TestGetManyKeysAllocs 快速路径 (调用方数组、无转义、未回退) 不分配.
func TestGetManyKeysAllocs(t *testing.T) {
	doc := `{"brand":"xy","x_event_id":"nh_ping","n":1,"o":{"a":[1,2]},"_x":"api"}`
	keys := []string{"x_event_id", "_ctime", "o", "brand"}
	var dst [4]Result
	allocs := testing.AllocsPerRun(1000, func() {
		r := GetManyKeys(doc, keys, dst[:0])
		if r[0].Str != "nh_ping" || r[1].Exists() || r[3].Str != "xy" {
			panic("unexpected result")
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs per run = %v, want 0", allocs)
	}
}

// FuzzGetManyKeys 任意输入与逐键 Get 对比; 第三个键重复第一个键, 覆盖请求内重复.
// 除模糊器给出的两个键外, 还从文档中引号之间的子串取候选键, 提高命中真实键的概率.
func FuzzGetManyKeys(f *testing.F) {
	f.Add(`{"a":1,"b":{"c":2},"a":3}`, "a", "b")
	f.Add(`xx{"brand":"xy","x_event_id":"e"}`, "x_event_id", "brand")
	f.Add(`{"a":"\"","b":[1,{"a":2}]}`, "a", "b")
	f.Add(`[{"a":1}]`, "0", "a")
	f.Fuzz(func(t *testing.T, doc, k1, k2 string) {
		keys := []string{k1, k2, k1}
		parts := strings.Split(doc, `"`)
		for i := 1; i < len(parts) && len(keys) < 12; i += 2 {
			keys = append(keys, parts[i])
		}
		if bad := mkCheck(t, doc, keys, nil); bad != 0 {
			t.Fatalf("%d mismatches", bad)
		}
	})
}
