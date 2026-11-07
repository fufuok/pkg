// Package jsongen forked from darjun/json-gen
package jsongen

import (
	"slices"
	"strconv"
)

// inlineMapCap 是 Map 内联键值槽位数. 16 覆盖 Web 日志、告警及 IP 详情等常见对象 (≤16 字段) 而不额外分配;
// 代价是 Map 本身约 1.2KiB, 逃逸到堆 (嵌套 PutMap、放入数组或被返回保存) 时无论字段多少都按此大小分配.
// 实测改为 8 可让大量 ≤8 字段子对象的批量场景省约 1/3 内存, 但 9~16 字段对象会多一次分配并变慢, 故保持 16.
const inlineMapCap = 16

type valueKind uint8

const (
	kindPlainString valueKind = iota // 无需转义的字符串, 序列化时直接包裹双引号
	kindRawLiteral                   // 已转义字符串或预格式化 JSON 字面量, 原样输出 str
	kindInt                          // 有符号十进制整数, 序列化时直接 AppendInt
	kindUint                         // 无符号十进制整数, 序列化时直接 AppendUint
	kindBool                         // 布尔字面量
	kindInterface                    // Value 接口实现 (如 *Map, *Array, RawBytes)
)

type elem struct {
	str  string
	val  Value
	num  uint64
	kind valueKind
}

func (e *elem) size() int {
	switch e.kind {
	case kindPlainString:
		return len(e.str) + 2
	case kindRawLiteral:
		return len(e.str)
	case kindInt:
		return intLen(int64(e.num))
	case kindUint:
		return uintLen(e.num)
	case kindBool:
		if e.num != 0 {
			return 4
		}
		return 5
	default:
		return e.val.Size()
	}
}

func (e *elem) serialize(buf []byte) []byte {
	switch e.kind {
	case kindPlainString:
		buf = append(buf, '"')
		buf = append(buf, e.str...)
		return append(buf, '"')
	case kindRawLiteral:
		return append(buf, e.str...)
	case kindInt:
		return strconv.AppendInt(buf, int64(e.num), 10)
	case kindUint:
		return strconv.AppendUint(buf, e.num, 10)
	case kindBool:
		if e.num != 0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	default:
		return e.val.Serialize(buf)
	}
}

func makeStringElem(s string) elem {
	if !needsJSONEscape(s) {
		return elem{str: s, kind: kindPlainString}
	}
	dst := make([]byte, 0, len(s)+8)
	return elem{str: string(appendEscapedJSONString(dst, s)), kind: kindRawLiteral}
}

func uintLen(u uint64) int {
	switch {
	case u < 10:
		return 1
	case u < 100:
		return 2
	case u < 1000:
		return 3
	case u < 10000:
		return 4
	case u < 100000:
		return 5
	case u < 1000000:
		return 6
	default:
		n := 7
		u /= 1000000
		for u >= 100 {
			u /= 100
			n += 2
		}
		if u >= 10 {
			n++
		}
		return n
	}
}

func intLen(i int64) int {
	if i < 0 {
		return 1 + uintLen(-uint64(i))
	}
	return uintLen(uint64(i))
}

type Value interface {
	// Serialize 将值序列化为字符串, 追加到 buf 并返回
	Serialize(buf []byte) []byte
	// Size 返回值最终字节数
	Size() int
}

// V 表示标准的 JSON 值，例如: 123，1.23，true 等
// 字符串值是以双引号包裹的字符串, 如: "abc"
type V string

func (v V) Serialize(buf []byte) []byte {
	return append(buf, v...)
}

func (v V) Size() int {
	return len(v)
}

// RawString 附加原生 JSON 数据字符串形式, 如直接附加: `[1,{"A":1}]`
type RawString string

func (s RawString) Serialize(buf []byte) []byte {
	return append(buf, s...)
}

func (s RawString) Size() int {
	return len(s)
}

// RawBytes 附加原生 JSON 数据
type RawBytes []byte

func (b RawBytes) Serialize(buf []byte) []byte {
	return append(buf, b...)
}

func (b RawBytes) Size() int {
	return len(b)
}

// Array 数组类的 JSON 数据
type Array struct {
	elems []elem
}

// NewArray 创建 JSON 数组, 无添加值时, 结果中至少附加一个空数组: []
func NewArray() *Array {
	return &Array{}
}

// Serialize 将数组序列化并追加到 buf 后返回. 只在 buf 已满 (含 nil 和非空满前缀) 时按 Size 一次性扩容,
// 嵌套调用时外层已预留容量, 不重复遍历计算 Size.
// buf 为 nil 时结果按 Size 由运行时一次分配 (容量按尺寸类取整) 而非取自 bytespool:
// 现有调用方均不归还结果, 池未命中反而更慢且按 2 的幂次多占内存.
// 结果不应 bytespool.Release (不来自池); 需要复用缓冲时请传入自有 buf 并自行管理生命周期.
func (a *Array) Serialize(buf []byte) []byte {
	if cap(buf)-len(buf) == 0 {
		buf = slices.Grow(buf, a.Size())
	}

	buf = append(buf, '[')
	count := len(a.elems)
	for i := range count {
		buf = a.elems[i].serialize(buf)
		if i != count-1 {
			buf = append(buf, ',')
		}
	}
	return append(buf, ']')
}

func (a *Array) Size() int {
	size := 2 // for []
	for i := range a.elems {
		size += a.elems[i].size()
	}
	if len(a.elems) > 1 {
		size += len(a.elems) - 1
	}
	return size
}

// AppendUint 追加单个或多个 uint64 到数组: [1,2] => [1,2,3,4]
func (a *Array) AppendUint(vv ...uint64) {
	for _, v := range vv {
		a.elems = append(a.elems, elem{num: v, kind: kindUint})
	}
}

// AppendInt 追加单个或多个 int64 到数组: [1,2] => [1,2,3,4]
func (a *Array) AppendInt(vv ...int64) {
	for _, v := range vv {
		a.elems = append(a.elems, elem{num: uint64(v), kind: kindInt})
	}
}

// AppendFloat 追加单个或多个 float64 到数组: [1,2] => [1,2,3.1,4]
func (a *Array) AppendFloat(vv ...float64) {
	for _, v := range vv {
		a.elems = append(a.elems, elem{str: strconv.FormatFloat(v, 'f', -1, 64), kind: kindRawLiteral})
	}
}

// AppendBool 追加单个或多个 bool 到数组: [1,2] => [1,2,true,false]
func (a *Array) AppendBool(vv ...bool) {
	for _, v := range vv {
		var n uint64
		if v {
			n = 1
		}
		a.elems = append(a.elems, elem{num: n, kind: kindBool})
	}
}

// AppendString 追加单个或多个 string 到数组: [1,2] => [1,2,"A","b"]
// a.AppendString("A", "b")
func (a *Array) AppendString(vv ...string) {
	for _, v := range vv {
		a.elems = append(a.elems, makeStringElem(v))
	}
}

// AppendMap 追加单个或多个 map 到数组: [1,2] => [1,2,{"A":1},{"b":true}]
func (a *Array) AppendMap(vv ...*Map) {
	for _, v := range vv {
		if v != nil {
			a.elems = append(a.elems, elem{val: v, kind: kindInterface})
		}
	}
}

// AppendArray 追加单个或多个 array 到数组: [1,2] => [1,2,[{"A":1}],[true]]
func (a *Array) AppendArray(vv ...*Array) {
	for _, v := range vv {
		if v != nil {
			a.elems = append(a.elems, elem{val: v, kind: kindInterface})
		}
	}
}

// AppendRawString 追加单个或多个原生 JSON 字符串, 如: [1,2] => [1,2,[2,{"A":1}]]
// a.AppendRawString(`[2,{"A":1}]`)
func (a *Array) AppendRawString(ss ...string) {
	for _, v := range ss {
		if v != "" {
			a.elems = append(a.elems, elem{str: v, kind: kindRawLiteral})
		}
	}
}

// AppendRawBytes 追加单个或多个原生 JSON 数据
func (a *Array) AppendRawBytes(bb ...[]byte) {
	for _, v := range bb {
		if v != nil {
			a.elems = append(a.elems, elem{val: RawBytes(v), kind: kindInterface})
		}
	}
}

// AppendUintArray 追加 uint64 数组: [1,2] => [1,2,[3,4,5]]
func (a *Array) AppendUintArray(vv []uint64) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendUint(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendIntArray 追加 int64 数组: [1,2] => [1,2,[3,4,5]]
func (a *Array) AppendIntArray(vv []int64) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendInt(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendFloatArray 追加 float64 数组: [1,2] => [1,2,[3,4.1,5]]
func (a *Array) AppendFloatArray(vv []float64) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendFloat(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendBoolArray 追加 bool 数组: [1,2] => [1,2,[true,false]]
func (a *Array) AppendBoolArray(vv []bool) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendBool(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendStringArray 追加 string 数组: [1,2] => [1,2,["A","b"]]
// a.AppendStringArray([]string{"A","b"})
func (a *Array) AppendStringArray(vv []string) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendString(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendMapArray 追加 map 数组: [1,2] => [1,2,[{"A":1},{"b":true}]]
func (a *Array) AppendMapArray(vv []*Map) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendMap(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendArrayArray 追加 array 数组: [1,2] => [1,2,[[3],[4],[{"b":true}]]]
func (a *Array) AppendArrayArray(vv []*Array) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendArray(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendRawStringArray 追加原生 JSON 字符串数组: [1,2] => [1,2,["x",[4],[{"b":true}]]]
// a.AppendRawStringArray([]string{`"x"`, `[4]`, `[{"b":true}]`})
func (a *Array) AppendRawStringArray(ss []string) {
	sub := &Array{elems: make([]elem, 0, len(ss))}
	sub.AppendRawString(ss...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

// AppendRawBytesArray 追加原生 JSON 数据数组
func (a *Array) AppendRawBytesArray(vv [][]byte) {
	sub := &Array{elems: make([]elem, 0, len(vv))}
	sub.AppendRawBytes(vv...)
	a.elems = append(a.elems, elem{val: sub, kind: kindInterface})
}

type kvEntry struct {
	key       string
	keyQuoted bool
	value     elem
}

// Map 对象类(字典) JSON 数据.
// 内联前 16 个键值槽位且无自引用指针, 常规热路径(如 IP 查询、Web 日志、告警构造)可直接在栈上完成组装.
type Map struct {
	n      int
	inline [inlineMapCap]kvEntry
	extra  []kvEntry
}

// NewMap 创建对象类(字典) JSON 数据集, 无添加值时, 结果中至少附加一个空对象: {}
func NewMap() *Map {
	return &Map{}
}

// Serialize 将对象序列化并追加到 buf 后返回, buf 容量与结果归属规则同 Array.Serialize.
func (m *Map) Serialize(buf []byte) []byte {
	if cap(buf)-len(buf) == 0 {
		buf = slices.Grow(buf, m.Size())
	}

	buf = append(buf, '{')
	total := m.n + len(m.extra)
	idx := 0
	for i := range m.n {
		buf = writeKV(buf, &m.inline[i], idx != total-1)
		idx++
	}
	for i := range m.extra {
		buf = writeKV(buf, &m.extra[i], idx != total-1)
		idx++
	}
	return append(buf, '}')
}

func writeKV(buf []byte, e *kvEntry, comma bool) []byte {
	if e.keyQuoted {
		buf = append(buf, e.key...)
		buf = append(buf, ':')
	} else {
		buf = append(buf, '"')
		buf = append(buf, e.key...)
		buf = append(buf, '"', ':')
	}
	buf = e.value.serialize(buf)
	if comma {
		buf = append(buf, ',')
	}
	return buf
}

func (m *Map) Size() int {
	size := 2 // for {}
	for i := range m.n {
		size += kvSize(&m.inline[i])
	}
	for i := range m.extra {
		size += kvSize(&m.extra[i])
	}
	total := m.n + len(m.extra)
	if total > 1 {
		size += total - 1
	}
	return size
}

func kvSize(e *kvEntry) int {
	keyLen := len(e.key) + 1 // +1 for ':'
	if !e.keyQuoted {
		keyLen += 2 // +2 for surrounding '"'
	}
	return keyLen + e.value.size()
}

func (m *Map) putElem(key string, val elem) {
	var entry kvEntry
	if !needsJSONEscape(key) {
		entry = kvEntry{key: key, value: val}
	} else {
		dst := make([]byte, 0, len(key)+8)
		entry = kvEntry{
			key:       string(appendEscapedJSONString(dst, key)),
			keyQuoted: true,
			value:     val,
		}
	}
	if m.n < inlineMapCap {
		m.inline[m.n] = entry
		m.n++
		return
	}
	m.extra = append(m.extra, entry)
}

func (m *Map) PutRawString(key, s string) {
	m.putElem(key, elem{str: s, kind: kindRawLiteral})
}

func (m *Map) PutRawBytes(key string, b []byte) {
	m.putElem(key, elem{val: RawBytes(b), kind: kindInterface})
}

func (m *Map) PutUint(key string, u uint64) {
	m.putElem(key, elem{num: u, kind: kindUint})
}

func (m *Map) PutInt(key string, i int64) {
	m.putElem(key, elem{num: uint64(i), kind: kindInt})
}

func (m *Map) PutFloat(key string, f float64) {
	m.putElem(key, elem{str: strconv.FormatFloat(f, 'f', -1, 64), kind: kindRawLiteral})
}

func (m *Map) PutBool(key string, b bool) {
	var n uint64
	if b {
		n = 1
	}
	m.putElem(key, elem{num: n, kind: kindBool})
}

func (m *Map) PutString(key, s string) {
	m.putElem(key, makeStringElem(s))
}

func (m *Map) PutRawStringArray(key string, ss []string) {
	a := &Array{elems: make([]elem, 0, len(ss))}
	a.AppendRawString(ss...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

func (m *Map) PutRawBytesArray(key string, bs [][]byte) {
	a := &Array{elems: make([]elem, 0, len(bs))}
	a.AppendRawBytes(bs...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

// PutUintArray 添加 uint64 数组数据项: {"A":[1,2]}
func (m *Map) PutUintArray(key string, u []uint64) {
	a := &Array{elems: make([]elem, 0, len(u))}
	a.AppendUint(u...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

func (m *Map) PutIntArray(key string, i []int64) {
	a := &Array{elems: make([]elem, 0, len(i))}
	a.AppendInt(i...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

func (m *Map) PutFloatArray(key string, f []float64) {
	a := &Array{elems: make([]elem, 0, len(f))}
	a.AppendFloat(f...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

func (m *Map) PutBoolArray(key string, b []bool) {
	a := &Array{elems: make([]elem, 0, len(b))}
	a.AppendBool(b...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

func (m *Map) PutStringArray(key string, s []string) {
	a := &Array{elems: make([]elem, 0, len(s))}
	a.AppendString(s...)
	m.putElem(key, elem{val: a, kind: kindInterface})
}

// PutArray 添加值为数组的数据项: {"A":[1,true,"x"]}
func (m *Map) PutArray(key string, oa *Array) {
	m.putElem(key, elem{val: oa, kind: kindInterface})
}

// PutMap 添加值为对象(字典)的数据项, map 嵌套: {"A":{"sub":1}}
func (m *Map) PutMap(key string, om *Map) {
	m.putElem(key, elem{val: om, kind: kindInterface})
}
