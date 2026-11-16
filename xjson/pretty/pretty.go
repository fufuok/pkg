package pretty

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// Options is Pretty options
type Options struct {
	// Width 是单行数组或对象的最大列宽, 小于等于 0 时不尝试单行输出.
	// Default is 80
	Width int
	// Prefix is a prefix for all lines
	// Default is an empty string
	Prefix string
	// Indent is the nested indentation
	// Default is two spaces
	Indent string
	// SortKeys will sort the keys alphabetically
	// Default is false
	SortKeys bool
}

// DefaultOptions is the default options for pretty formats.
var DefaultOptions = &Options{Width: 80, Prefix: "", Indent: "  ", SortKeys: false}

// Pretty 将输入 JSON 格式化为更易读的文本, 通过换行和缩进展示各元素.
func Pretty(json []byte) []byte { return PrettyOptions(json, nil) }

// PrettyOptions is like Pretty but with customized options.
func PrettyOptions(json []byte, opts *Options) []byte {
	if opts == nil {
		opts = DefaultOptions
	}
	buf := make([]byte, 0, len(json))
	if len(opts.Prefix) != 0 {
		buf = append(buf, opts.Prefix...)
	}
	buf, _, _, _ = appendPrettyAny(buf, json, 0, true,
		opts.Width, opts.Prefix, opts.Indent, opts.SortKeys,
		0, 0, -1)
	if len(buf) > 0 {
		buf = append(buf, '\n')
	}
	return buf
}

// Ugly removes insignificant space characters from the input json byte slice
// and returns the compacted result.
//
// 返回值总是新分配的独立副本, 即使输入本来就没有可删除的空白也不会与 json 共用底层数组.
func Ugly(json []byte) []byte {
	n := uglyPrefixLen(json)
	buf := make([]byte, 0, len(json))
	// 前缀无需压缩, 整段复制后再从第一个可删除字节继续.
	buf = append(buf, json[:n]...)
	return uglyTail(buf[:len(json)], json, n, n)
}

// UglyInPlace removes insignificant space characters from the input json
// byte slice and returns the compacted result. This method reuses the
// input json buffer to avoid allocations. Do not use the original bytes
// slice upon return.
//
// 输入没有可删除的空白时直接返回 json 本身, 不写内存.
func UglyInPlace(json []byte) []byte {
	n := uglyPrefixLen(json)
	// 前缀已在原位, 从第一个可删除字节开始原地压紧.
	return uglyTail(json, json, n, n)
}

// uglyPrefixLen 只读扫描 src, 返回字符串外第一个可删除字节 (<= ' ') 的下标; 没有时返回 len(src).
// 字符串外的空白之外, 其余字节在压缩前后位置不变, 因此该下标之前的内容可原样保留.
// 字符串直到末尾都未闭合时, 剩余内容全部属于字符串, 同样返回 len(src).
// 热路径全部内联在一个循环里: 短字符串若调用独立的扫描函数, 调用开销会超过节省的写入.
func uglyPrefixLen(src []byte) int {
	n := len(src)
outer:
	for i := 0; i < n; {
		c := src[i]
		i++
		if c <= ' ' {
			return i - 1
		}
		if c != '"' {
			continue
		}
		// 字符串前 16 字节逐字节扫描, 反斜杠连同其后字节一起跳过 (等价于按引号前反斜杠奇偶判定).
		for lim := min(i+16, n); i < lim; {
			c = src[i]
			i++
			if c == '"' {
				continue outer
			}
			if c == '\\' {
				i++
			}
		}
		// 长字符串: IndexByte 跳到下一个引号, 再按其前连续反斜杠的奇偶判定是否闭合.
		// 只用 IndexByte 时, 大量短字符串的调用开销反而比逐字节慢.
		for i < n {
			q := bytes.IndexByte(src[i:], '"')
			if q < 0 {
				break
			}
			i += q + 1
			if !uglyQuoteEscaped(src, i-1) {
				continue outer
			}
		}
		// 未闭合字符串 (含末尾孤立反斜杠使 i 越过 n): 余下全部属于字符串.
		return n
	}
	return n
}

// uglyTail 从 src[i] 起继续压缩, 要求 out[:w] 已是 src[:i] 的压缩结果且 src[i] 位于字符串外.
// out 长度须为 len(src), 可与 src 共用底层数组且起点相同 (原地压缩); 此时 w <= i 恒成立,
// 写入位置不会超过读取位置, 尚未读取的 src 字节不会被覆盖. 返回 out[:最终写入长度].
func uglyTail(out, src []byte, w, i int) []byte {
	n := len(src)
outer:
	for i < n {
		c := src[i]
		i++
		if c <= ' ' {
			// 字符串外的空白和控制字符全部删除; 先判空白, 缩进密集的输入每字节只走一次分支.
			continue
		}
		out[w] = c
		w++
		if c != '"' {
			continue
		}
		// 字符串前 16 字节逐字节复制, 反斜杠连同其后字节一起复制, 不把被转义的引号当作闭合.
		for lim := min(i+16, n); i < lim; {
			c = src[i]
			i++
			out[w] = c
			w++
			if c == '"' {
				continue outer
			}
			if c == '\\' && i < n {
				out[w] = src[i]
				w++
				i++
			}
		}
		// 长字符串: 整段复制到下一个引号 (copy 按 memmove 处理原地重叠), 再在 out 上回溯反斜杠判定是否闭合.
		// 原地压缩时 src 中本字符串已复制过的部分可能已被覆盖, 只有 out 中的副本可靠;
		// 回溯最远停在 out 中的开引号上.
		for i < n {
			q := bytes.IndexByte(src[i:], '"')
			if q < 0 {
				break
			}
			w += copy(out[w:], src[i:i+q+1])
			i += q + 1
			if !uglyQuoteEscaped(out, w-1) {
				continue outer
			}
		}
		// 未闭合字符串: 剩余内容原样保留.
		w += copy(out[w:], src[i:])
		break
	}
	return out[:w]
}

// uglyQuoteEscaped 判断字符串内 src[q] 处的引号是否被转义, 即紧邻其前的连续反斜杠为奇数个.
// 回溯最远停在开引号上 (开引号不是反斜杠), 因此不会越过字符串起点, 也无需额外下界.
// 每段反斜杠只属于一个引号, 总回溯量与输入长度呈线性.
func uglyQuoteEscaped(src []byte, q int) bool {
	j := q - 1
	for src[j] == '\\' {
		j--
	}
	return (q-1-j)&1 == 1
}

func isNaNOrInf(src []byte) bool {
	return src[0] == 'i' || // Inf
		src[0] == 'I' || // inf
		src[0] == '+' || // +Inf
		src[0] == 'N' || // Nan
		(src[0] == 'n' && len(src) > 1 && src[1] != 'u') // nan
}

func appendPrettyAny(buf, json []byte, i int, pretty bool, width int, prefix, indent string, sortkeys bool, tabs, nl, max int) ([]byte, int, int, bool) {
	for ; i < len(json); i++ {
		if json[i] <= ' ' {
			continue
		}
		if json[i] == '"' {
			return appendPrettyString(buf, json, i, nl)
		}

		if (json[i] >= '0' && json[i] <= '9') || json[i] == '-' || isNaNOrInf(json[i:]) {
			return appendPrettyNumber(buf, json, i, nl)
		}
		if json[i] == '{' {
			return appendPrettyObject(buf, json, i, '{', '}', pretty, width, prefix, indent, sortkeys, tabs, nl, max)
		}
		if json[i] == '[' {
			return appendPrettyObject(buf, json, i, '[', ']', pretty, width, prefix, indent, sortkeys, tabs, nl, max)
		}
		switch json[i] {
		case 't':
			return append(buf, 't', 'r', 'u', 'e'), i + 4, nl, true
		case 'f':
			return append(buf, 'f', 'a', 'l', 's', 'e'), i + 5, nl, true
		case 'n':
			return append(buf, 'n', 'u', 'l', 'l'), i + 4, nl, true
		}
	}
	return buf, i, nl, true
}

type pair struct {
	kstart, kend int
	vstart, vend int
}

type byKeyVal struct {
	sorted bool
	json   []byte
	buf    []byte
	pairs  []pair
}

func (arr *byKeyVal) Len() int {
	return len(arr.pairs)
}

func (arr *byKeyVal) Less(i, j int) bool {
	if arr.isLess(i, j, byKey) {
		return true
	}
	if arr.isLess(j, i, byKey) {
		return false
	}
	return arr.isLess(i, j, byVal)
}

func (arr *byKeyVal) Swap(i, j int) {
	arr.pairs[i], arr.pairs[j] = arr.pairs[j], arr.pairs[i]
	arr.sorted = true
}

type byKind int

const (
	byKey byKind = 0
	byVal byKind = 1
)

type jtype int

const (
	jnull jtype = iota
	jfalse
	jnumber
	jstring
	jtrue
	jjson
)

func getjtype(v []byte) jtype {
	if len(v) == 0 {
		return jnull
	}
	switch v[0] {
	case '"':
		return jstring
	case 'f':
		return jfalse
	case 't':
		return jtrue
	case 'n':
		return jnull
	case '[', '{':
		return jjson
	default:
		return jnumber
	}
}

func (arr *byKeyVal) isLess(i, j int, kind byKind) bool {
	k1 := arr.json[arr.pairs[i].kstart:arr.pairs[i].kend]
	k2 := arr.json[arr.pairs[j].kstart:arr.pairs[j].kend]
	var v1, v2 []byte
	if kind == byKey {
		v1 = k1
		v2 = k2
	} else {
		v1 = bytes.TrimSpace(arr.buf[arr.pairs[i].vstart:arr.pairs[i].vend])
		v2 = bytes.TrimSpace(arr.buf[arr.pairs[j].vstart:arr.pairs[j].vend])
		if len(v1) >= len(k1)+1 {
			v1 = bytes.TrimSpace(v1[len(k1)+1:])
		}
		if len(v2) >= len(k2)+1 {
			v2 = bytes.TrimSpace(v2[len(k2)+1:])
		}
	}
	t1 := getjtype(v1)
	t2 := getjtype(v2)
	if t1 < t2 {
		return true
	}
	if t1 > t2 {
		return false
	}
	if t1 == jstring {
		s1 := parsestr(v1)
		s2 := parsestr(v2)
		return string(s1) < string(s2)
	}
	if t1 == jnumber {
		n1, _ := strconv.ParseFloat(string(v1), 64)
		n2, _ := strconv.ParseFloat(string(v2), 64)
		return n1 < n2
	}
	return string(v1) < string(v2)
}

func parsestr(s []byte) []byte {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			var str string
			json.Unmarshal(s, &str)
			return []byte(str)
		}
		if s[i] == '"' {
			return s[1:i]
		}
	}
	return nil
}

func appendPrettyObject(buf, json []byte, i int, open, close byte, pretty bool,
	width int, prefix, indent string, sortkeys bool, tabs, nl, max int,
) ([]byte, int, int, bool) {
	var ok bool
	if width > 0 {
		if pretty && (open == '[' || open == '{') && max == -1 {
			// here we try to create a single line array
			max := width - (len(buf) - nl)
			if max > 3 {
				s1, s2 := len(buf), i
				buf, i, _, ok = appendPrettyObject(buf, json, i, open, close,
					false, width, prefix, "", sortkeys, 0, 0, max)
				if ok && len(buf)-s1 <= max {
					return buf, i, nl, true
				}
				buf = buf[:s1]
				i = s2
			}
		}
	}
	// start 记录本容器在 buf 中的起点, 单行试排 (max != -1) 时据此判断是否已超出可用宽度.
	start := len(buf)
	buf = append(buf, open)
	i++
	var pairs []pair
	if open == '{' && sortkeys {
		pairs = make([]pair, 0, 8)
	}
	var n int
	for ; i < len(json); i++ {
		if json[i] <= ' ' {
			continue
		}
		if json[i] == close {
			if open == '{' && sortkeys {
				buf = sortPairs(json, buf, pairs, pretty)
			}
			if pretty {
				if n > 0 {
					nl = len(buf)
					if buf[nl-1] == ' ' {
						buf[nl-1] = '\n'
					} else {
						buf = append(buf, '\n')
					}
				}
				if buf[len(buf)-1] != open {
					buf = appendTabs(buf, prefix, indent, tabs)
				}
			} else if open == '{' {
				buf = append(buf, ' ')
			}
			buf = append(buf, close)
			return buf, i + 1, nl, true
		}
		if open == '[' || json[i] == '"' {
			if n > 0 {
				buf = append(buf, ',')
				if width != -1 {
					buf = append(buf, ' ')
				}
			} else if open == '{' {
				if width != -1 {
					buf = append(buf, ' ')
				}
			}
			var p pair
			if pretty {
				nl = len(buf)
				if buf[nl-1] == ' ' {
					buf[nl-1] = '\n'
				} else {
					buf = append(buf, '\n')
				}
				if open == '{' && sortkeys {
					p.kstart = i
					p.vstart = len(buf)
				}
				buf = appendTabs(buf, prefix, indent, tabs+1)
			} else {
				if open == '{' && sortkeys {
					p.kstart = i
					p.vstart = len(buf)
				}
			}
			if open == '{' {
				buf, i, nl, _ = appendPrettyString(buf, json, i, nl)
				if sortkeys {
					p.kend = i
				}
				buf = append(buf, ':')
				buf = append(buf, ' ')
			}
			buf, i, nl, ok = appendPrettyAny(buf, json, i, pretty, width,
				prefix, indent, sortkeys, tabs+1, nl, max)
			// 本地修正: 上游试排单行时会完整渲染整个子树后才比较宽度, 放不下时整段丢弃,
			// 嵌套越深重复越多. 已超出可用宽度即提前放弃, 结果本就会被丢弃, 输出不变.
			if max != -1 && (!ok || len(buf)-start > max) {
				return buf, i, nl, false
			}
			if open == '{' && sortkeys {
				p.vend = len(buf)
				if p.kstart > p.kend || p.vstart > p.vend {
					// bad data. disable sorting
					sortkeys = false
				} else {
					pairs = append(pairs, p)
				}
			}
			i--
			n++
		}
	}
	return buf, i, nl, open != '{'
}

func sortPairs(json, buf []byte, pairs []pair, pretty bool) []byte {
	if len(pairs) == 0 {
		return buf
	}
	vstart := pairs[0].vstart
	vend := pairs[len(pairs)-1].vend
	arr := byKeyVal{false, json, buf, pairs}
	sort.Stable(&arr)
	if !arr.sorted {
		return buf
	}
	nbuf := make([]byte, 0, vend-vstart)
	for i, p := range pairs {
		nbuf = append(nbuf, buf[p.vstart:p.vend]...)
		if i < len(pairs)-1 {
			nbuf = append(nbuf, ',')
			if pretty {
				nbuf = append(nbuf, '\n')
			} else {
				nbuf = append(nbuf, ' ')
			}
		}
	}
	return append(buf[:vstart], nbuf...)
}

func appendPrettyString(buf, json []byte, i, nl int) ([]byte, int, int, bool) {
	s := i
	i++
	for ; i < len(json); i++ {
		if json[i] == '"' {
			var sc int
			for j := i - 1; j > s; j-- {
				if json[j] == '\\' {
					sc++
				} else {
					break
				}
			}
			if sc%2 == 1 {
				continue
			}
			i++
			break
		}
	}
	return append(buf, json[s:i]...), i, nl, true
}

func appendPrettyNumber(buf, json []byte, i, nl int) ([]byte, int, int, bool) {
	s := i
	i++
	for ; i < len(json); i++ {
		if json[i] <= ' ' || json[i] == ',' || json[i] == ':' || json[i] == ']' || json[i] == '}' {
			break
		}
	}
	return append(buf, json[s:i]...), i, nl, true
}

func appendTabs(buf []byte, prefix, indent string, tabs int) []byte {
	if len(prefix) != 0 {
		buf = append(buf, prefix...)
	}
	if len(indent) == 2 && indent[0] == ' ' && indent[1] == ' ' {
		for range tabs {
			buf = append(buf, ' ', ' ')
		}
	} else {
		for range tabs {
			buf = append(buf, indent...)
		}
	}
	return buf
}

// Style is the color style
type Style struct {
	Key, String, Number [2]string
	True, False, Null   [2]string
	Escape              [2]string
	Brackets            [2]string
	Append              func(dst []byte, c byte) []byte
}

func hexp(p byte) byte {
	switch {
	case p < 10:
		return p + '0'
	default:
		return (p - 10) + 'a'
	}
}

// TerminalStyle is for terminals
var TerminalStyle *Style

func init() {
	TerminalStyle = &Style{
		Key:      [2]string{"\x1B[1m\x1B[94m", "\x1B[0m"},
		String:   [2]string{"\x1B[32m", "\x1B[0m"},
		Number:   [2]string{"\x1B[33m", "\x1B[0m"},
		True:     [2]string{"\x1B[36m", "\x1B[0m"},
		False:    [2]string{"\x1B[36m", "\x1B[0m"},
		Null:     [2]string{"\x1B[2m", "\x1B[0m"},
		Escape:   [2]string{"\x1B[35m", "\x1B[0m"},
		Brackets: [2]string{"\x1B[1m", "\x1B[0m"},
		Append: func(dst []byte, c byte) []byte {
			if c < ' ' && (c != '\r' && c != '\n' && c != '\t' && c != '\v') {
				dst = append(dst, "\\u00"...)
				dst = append(dst, hexp((c>>4)&0xF))
				return append(dst, hexp(c&0xF))
			}
			return append(dst, c)
		},
	}
}

// Color 按 style 为 JSON 添加颜色; style 为 nil 时使用默认的 TerminalStyle.
func Color(src []byte, style *Style) []byte {
	if style == nil {
		style = TerminalStyle
	}
	apnd := style.Append
	if apnd == nil {
		apnd = func(dst []byte, c byte) []byte {
			return append(dst, c)
		}
	}
	type stackt struct {
		kind byte
		key  bool
	}
	var dst []byte
	var stack []stackt
	for i := 0; i < len(src); i++ {
		if src[i] == '"' {
			key := len(stack) > 0 && stack[len(stack)-1].key
			if key {
				dst = append(dst, style.Key[0]...)
			} else {
				dst = append(dst, style.String[0]...)
			}
			dst = apnd(dst, '"')
			esc := 0
			for i = i + 1; i < len(src); i++ {
				if esc > 0 {
					dst = apnd(dst, src[i])
					esc--
					if esc == 0 {
						dst = append(dst, style.Escape[1]...)
						if key {
							dst = append(dst, style.Key[0]...)
						} else {
							dst = append(dst, style.String[0]...)
						}
					}
				} else if src[i] == '\\' {
					if key {
						dst = append(dst, style.Key[1]...)
					} else {
						dst = append(dst, style.String[1]...)
					}
					dst = append(dst, style.Escape[0]...)
					dst = apnd(dst, src[i])
					if i+1 < len(src) && src[i+1] == 'u' {
						esc = 5
					} else {
						esc = 1
					}
				} else {
					dst = apnd(dst, src[i])
				}
				if src[i] == '"' {
					j := i - 1
					for ; ; j-- {
						if src[j] != '\\' {
							break
						}
					}
					if (j-i)%2 != 0 {
						break
					}
				}
			}
			if esc > 0 {
				dst = append(dst, style.Escape[1]...)
			} else if key {
				dst = append(dst, style.Key[1]...)
			} else {
				dst = append(dst, style.String[1]...)
			}
		} else if src[i] == '{' || src[i] == '[' {
			stack = append(stack, stackt{src[i], src[i] == '{'})
			dst = append(dst, style.Brackets[0]...)
			dst = apnd(dst, src[i])
			dst = append(dst, style.Brackets[1]...)
		} else if (src[i] == '}' || src[i] == ']') && len(stack) > 0 {
			stack = stack[:len(stack)-1]
			dst = append(dst, style.Brackets[0]...)
			dst = apnd(dst, src[i])
			dst = append(dst, style.Brackets[1]...)
		} else if (src[i] == ':' || src[i] == ',') && len(stack) > 0 && stack[len(stack)-1].kind == '{' {
			stack[len(stack)-1].key = !stack[len(stack)-1].key
			dst = append(dst, style.Brackets[0]...)
			dst = apnd(dst, src[i])
			dst = append(dst, style.Brackets[1]...)
		} else {
			var kind byte
			if (src[i] >= '0' && src[i] <= '9') || src[i] == '-' || isNaNOrInf(src[i:]) {
				kind = '0'
				dst = append(dst, style.Number[0]...)
			} else if src[i] == 't' {
				kind = 't'
				dst = append(dst, style.True[0]...)
			} else if src[i] == 'f' {
				kind = 'f'
				dst = append(dst, style.False[0]...)
			} else if src[i] == 'n' {
				kind = 'n'
				dst = append(dst, style.Null[0]...)
			} else {
				dst = apnd(dst, src[i])
			}
			if kind != 0 {
				for ; i < len(src); i++ {
					if src[i] <= ' ' || src[i] == ',' || src[i] == ':' || src[i] == ']' || src[i] == '}' {
						i--
						break
					}
					dst = apnd(dst, src[i])
				}
				if kind == '0' {
					dst = append(dst, style.Number[1]...)
				} else if kind == 't' {
					dst = append(dst, style.True[1]...)
				} else if kind == 'f' {
					dst = append(dst, style.False[1]...)
				} else if kind == 'n' {
					dst = append(dst, style.Null[1]...)
				}
			}
		}
	}
	return dst
}

// Spec 将注释和尾逗号替换为空白, 供普通 JSON 解析器处理扩展输入.
//
// 输入的注释和字符串语法完整时, 保留长度和换行位置, 便于外部解析器定位原始偏移.
// 本函数不校验 JSON 合法性, 也不修复其他语法错误或未闭合注释.
// JSON 规范: https://tools.ietf.org/html/rfc8259
func Spec(src []byte) []byte {
	return spec(src, nil)
}

// SpecInPlace is the same as Spec, but this method reuses the input json
// buffer to avoid allocations. Do not use the original bytes slice upon return.
func SpecInPlace(src []byte) []byte {
	return spec(src, src)
}

func spec(src, dst []byte) []byte {
	dst = dst[:0]
	for i := 0; i < len(src); i++ {
		if src[i] == '/' {
			if i < len(src)-1 {
				if src[i+1] == '/' {
					dst = append(dst, ' ', ' ')
					i += 2
					for ; i < len(src); i++ {
						if src[i] == '\n' {
							dst = append(dst, '\n')
							break
						} else if src[i] == '\t' || src[i] == '\r' {
							dst = append(dst, src[i])
						} else {
							dst = append(dst, ' ')
						}
					}
					continue
				}
				if src[i+1] == '*' {
					dst = append(dst, ' ', ' ')
					i += 2
					for ; i < len(src)-1; i++ {
						if src[i] == '*' && src[i+1] == '/' {
							dst = append(dst, ' ', ' ')
							i++
							break
						} else if src[i] == '\n' || src[i] == '\t' ||
							src[i] == '\r' {
							dst = append(dst, src[i])
						} else {
							dst = append(dst, ' ')
						}
					}
					continue
				}
			}
		}
		dst = append(dst, src[i])
		if src[i] == '"' {
			for i = i + 1; i < len(src); i++ {
				dst = append(dst, src[i])
				if src[i] == '"' {
					j := i - 1
					for ; ; j-- {
						if src[j] != '\\' {
							break
						}
					}
					if (j-i)%2 != 0 {
						break
					}
				}
			}
		} else if src[i] == '}' || src[i] == ']' {
			for j := len(dst) - 2; j >= 0; j-- {
				if dst[j] <= ' ' {
					continue
				}
				if dst[j] == ',' {
					dst[j] = ' '
				}
				break
			}
		}
	}
	return dst
}
