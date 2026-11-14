package gjson

import (
	"strconv"
	"strings"
	"testing"
)

// mkBenchSink 防止编译器消除基准中的查询.
var mkBenchSink Result

// mkBenchDoc 生成含 n 个顶层字段 k0..k{n-1} 的已压紧对象, 值轮换字符串、整数、浮点、布尔和嵌套对象.
func mkBenchDoc(n int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"k` + strconv.Itoa(i) + `":`)
		switch i % 5 {
		case 0:
			b.WriteString(`"value-` + strconv.Itoa(i) + `-abcdefgh"`)
		case 1:
			b.WriteString(strconv.Itoa(i * 7919))
		case 2:
			b.WriteString(`12.345`)
		case 3:
			b.WriteString(`true`)
		default:
			b.WriteString(`{"x":"nested","y":[1,2,3],"z":{"w":null}}`)
		}
	}
	b.WriteByte('}')
	return b.String()
}

// mkBenchKeys 按位置生成 n 个请求键:
//   - front: 文档前 n 个字段; back: 文档后 n 个字段; missing: 全部不存在 (须扫完整个对象);
//   - mixed: 一半取文档前部字段, 一半不存在, 近似 "日期字段在前, _ctime 缺失" 的场景.
//
// 返回 false 表示该组合超出文档字段数, 跳过.
func mkBenchKeys(fields, n int, pos string) ([]string, bool) {
	keys := make([]string, 0, n)
	switch pos {
	case "front":
		if n > fields {
			return nil, false
		}
		for i := range n {
			keys = append(keys, "k"+strconv.Itoa(i))
		}
	case "back":
		if n > fields {
			return nil, false
		}
		for i := fields - n; i < fields; i++ {
			keys = append(keys, "k"+strconv.Itoa(i))
		}
	case "missing":
		for i := range n {
			keys = append(keys, "m"+strconv.Itoa(i))
		}
	case "mixed":
		half := n / 2
		if half == 0 || half > fields {
			return nil, false
		}
		for i := range half {
			keys = append(keys, "k"+strconv.Itoa(i))
		}
		for i := half; i < n; i++ {
			keys = append(keys, "m"+strconv.Itoa(i))
		}
	}
	return keys, true
}

// BenchmarkGetManyKeys 对比逐键 Get 与单遍 GetManyKeys, 矩阵覆盖文档字段数、请求键数和键位置.
// 两种写法都把结果写入调用方数组, 差异只来自扫描次数; 键数为 1 时用于确认单键不应选用 GetManyKeys.
func BenchmarkGetManyKeys(b *testing.B) {
	for _, fields := range []int{8, 32, 128} {
		doc := mkBenchDoc(fields)
		for _, n := range []int{1, 2, 4, 8, 16, 64} {
			for _, pos := range []string{"front", "back", "missing", "mixed"} {
				keys, ok := mkBenchKeys(fields, n, pos)
				if !ok {
					continue
				}
				prefix := "fields=" + strconv.Itoa(fields) + "/keys=" + strconv.Itoa(n) + "/pos=" + pos
				dst := make([]Result, n)
				b.Run(prefix+"/Get", func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(doc)))
					for b.Loop() {
						for i, k := range keys {
							dst[i] = Get(doc, k)
						}
						mkBenchSink = dst[n-1]
					}
				})
				b.Run(prefix+"/GetManyKeys", func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(doc)))
					for b.Loop() {
						mkBenchSink = GetManyKeys(doc, keys, dst[:0])[n-1]
					}
				})
			}
		}
	}
}
