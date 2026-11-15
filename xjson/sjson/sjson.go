// Package sjson provides setting json values.
package sjson

import (
	jsongo "encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"github.com/fufuok/pkg/xjson/gjson"
)

// maxpad 限制单次 Set 调用中补齐数组可追加的 null 总数, 防止超大索引耗尽内存.
// 本地修正: 上游只限制每层, 路径 "a" 后重复 100 次 ".100000" (701 字节) 仍会生成约 50MB JSON.
const maxpad = 100000

type errorType struct {
	msg string
}

func (err *errorType) Error() string {
	return err.msg
}

// Options represents additional options for the Set and Delete functions.
type Options struct {
	// Optimistic is a hint that the value likely exists which
	// allows for the sjson to perform a fast-track search and replace.
	Optimistic bool
	// ReplaceInPlace 提示复用输入 JSON 切片, 而非重新分配结果切片.
	// 此时原输入 JSON 可能已被覆盖, 调用方不应继续使用原切片.
	// In the case when the destination slice doesn't have enough free
	// bytes to replace the data in place, a new bytes slice will be
	// created under the hood.
	// The Optimistic flag must be set to true and the input must be a
	// byte slice in order to use this field.
	ReplaceInPlace bool
}

type pathResult struct {
	part  string // current key part
	gpart string // gjson get part
	path  string // remaining path
	force bool   // force a string key
	more  bool   // there is more path to parse
}

func isSimpleChar(ch byte) bool {
	switch ch {
	case '|', '#', '@', '*', '?':
		return false
	default:
		return true
	}
}

func parsePath(path string) (res pathResult, simple bool) {
	var r pathResult
	if len(path) > 0 && path[0] == ':' {
		r.force = true
		path = path[1:]
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			r.part = path[:i]
			r.gpart = path[:i]
			r.path = path[i+1:]
			r.more = true
			return r, true
		}
		if !isSimpleChar(path[i]) {
			return r, false
		}
		if path[i] == '\\' {
			// go into escape mode. this is a slower path that
			// strips off the escape character from the part.
			epart := []byte(path[:i])
			gpart := []byte(path[:i+1])
			i++
			if i < len(path) {
				epart = append(epart, path[i])
				gpart = append(gpart, path[i])
				i++
				for ; i < len(path); i++ {
					if path[i] == '\\' {
						gpart = append(gpart, '\\')
						i++
						if i < len(path) {
							epart = append(epart, path[i])
							gpart = append(gpart, path[i])
						}
						continue
					} else if path[i] == '.' {
						r.part = string(epart)
						r.gpart = string(gpart)
						r.path = path[i+1:]
						r.more = true
						return r, true
					} else if !isSimpleChar(path[i]) {
						return r, false
					}
					epart = append(epart, path[i])
					gpart = append(gpart, path[i])
				}
			}
			// append the last part
			r.part = string(epart)
			r.gpart = string(gpart)
			return r, true
		}
	}
	r.part = path
	r.gpart = path
	return r, true
}

func mustMarshalString(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > 0x7f || s[i] == '"' || s[i] == '\\' {
			return true
		}
	}
	return false
}

// appendStringify makes a json string and appends to buf.
func appendStringify(buf []byte, s string) []byte {
	if mustMarshalString(s) {
		b, _ := jsongo.Marshal(s)
		return append(buf, b...)
	}
	buf = append(buf, '"')
	buf = append(buf, s...)
	buf = append(buf, '"')
	return buf
}

// makePadError 构造数组补齐超限错误; nels 为原数组已有元素数, 新建数组时为 0.
func makePadError(key string, nels int) error {
	return &errorType{
		fmt.Sprintf("cannot pad array for key '%s', has %d elements, "+
			"limit is %d", key, nels, maxpad),
	}
}

// makeTotalPadError 构造多层路径累计补齐超限错误; total 为加上本层后的累计补齐数.
func makeTotalPadError(key string, total int) error {
	return &errorType{
		fmt.Sprintf("cannot pad array for key '%s', total padding %d "+
			"exceeds limit %d", key, total, maxpad),
	}
}

// appendBuild builds a json block from a json path.
// padded 为本次调用此前已补齐的 null 数; 累计超过 maxpad 时返回错误, 调用方应放弃本次修改并保留原 JSON.
func appendBuild(buf []byte, array bool, paths []pathResult, raw string,
	stringify bool, padded int,
) ([]byte, error) {
	var err error
	if !array {
		buf = appendStringify(buf, paths[0].part)
		buf = append(buf, ':')
	}
	if len(paths) > 1 {
		n, numeric := atoui(paths[1])
		if numeric || (!paths[1].force && paths[1].part == "-1") {
			buf = append(buf, '[')
			// 先判本层: 单层超限沿用上游错误信息, 且 n 可达 math.MaxInt, 此时 padded+n 会溢出成负数.
			// 本层未超限时 n <= maxpad 且 padded <= maxpad, 累计值不会溢出.
			if n > maxpad {
				return nil, makePadError(paths[1].part, 0)
			}
			if n > maxpad-padded {
				return nil, makeTotalPadError(paths[1].part, padded+n)
			}
			buf = appendRepeat(buf, "null,", n)
			buf, err = appendBuild(buf, true, paths[1:], raw, stringify, padded+n)
			if err != nil {
				return nil, err
			}
			buf = append(buf, ']')
		} else {
			buf = append(buf, '{')
			buf, err = appendBuild(buf, false, paths[1:], raw, stringify, padded)
			if err != nil {
				return nil, err
			}
			buf = append(buf, '}')
		}
	} else {
		if stringify {
			buf = appendStringify(buf, raw)
		} else {
			buf = append(buf, raw...)
		}
	}
	return buf, nil
}

// atoui 将十进制路径片段解析为整数索引, 拒绝强制键名和包含非数字字符的片段.
// 超过 8 位时改用 ParseUint 检测溢出, 超出 int 范围的片段视为非数字键, 避免回绕成小索引.
func atoui(r pathResult) (n int, ok bool) {
	if r.force {
		return 0, false
	}
	// 先确认全为数字: 上游对超过 8 字节的片段直接调用 ParseUint, "q_utm_source" 这类普通键名
	// 每次都会生成一个错误对象 (2 次分配). 空片段沿用上游语义, 视为 0.
	for i := 0; i < len(r.part); i++ {
		if r.part[i] < '0' || r.part[i] > '9' {
			return 0, false
		}
	}
	if len(r.part) > 8 {
		n, err := strconv.ParseUint(r.part, 10, 64)
		return int(n), err == nil && n <= math.MaxInt
	}
	for i := 0; i < len(r.part); i++ {
		n = n*10 + int(r.part[i]-'0')
	}
	return n, true
}

// appendRepeat repeats string "n" times and appends to buf.
func appendRepeat(buf []byte, s string, n int) []byte {
	for range n {
		buf = append(buf, s...)
	}
	return buf
}

// trim does a rip trim
func trim(s string) string {
	for len(s) > 0 {
		if s[0] <= ' ' {
			s = s[1:]
			continue
		}
		break
	}
	for len(s) > 0 {
		if s[len(s)-1] <= ' ' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// deleteTailItem deletes the previous key or comma.
func deleteTailItem(buf []byte) ([]byte, bool) {
loop:
	for i := len(buf) - 1; i >= 0; i-- {
		// look for either a ',',':','['
		switch buf[i] {
		case '[':
			return buf, true
		case ',':
			return buf[:i], false
		case ':':
			// delete tail string
			i--
			for ; i >= 0; i-- {
				if buf[i] == '"' {
					i--
					for ; i >= 0; i-- {
						if buf[i] == '"' {
							i--
							if i >= 0 && buf[i] == '\\' {
								// 本地修正: 上游此处多执行一次 i--, 键以转义引号开头时会越过真正的起始引号,
								// 误删前一个键或留下非法 JSON. 交给循环的 i-- 回到反斜杠之前即可.
								continue
							}
							for ; i >= 0; i-- {
								// look for either a ',','{'
								switch buf[i] {
								case '{':
									return buf[:i+1], true
								case ',':
									return buf[:i], false
								}
							}
						}
					}
					break
				}
			}
			break loop
		}
	}
	return buf, false
}

var errNoChange = &errorType{"no change"}

// isStringValue 报告 jstr 是否恰好是一个 JSON 字符串值 (前后可有空白; 未闭合也算, 其后全在字符串内).
// 只用于决定是否继续向下取值: 字符串内容里的 '[' '{' 不是文档结构, 不能被当作容器.
// 嵌套取值时 jstr 是字符串的 Raw, 必然满足; 顶层文档以字符串开头但其后还有内容 (如 "k":{...} 这类片段)
// 时不算, 保持上游跳到其中第一个 '{' 或 '[' 的处理, 不把整个文档替换掉.
func isStringValue(jstr string) bool {
	i := 0
	for i < len(jstr) && jstr[i] <= ' ' {
		i++
	}
	if i == len(jstr) || jstr[i] != '"' {
		return false
	}
	for i++; i < len(jstr); i++ {
		if jstr[i] == '\\' {
			i++
		} else if jstr[i] == '"' {
			break
		}
	}
	for i++; i < len(jstr); i++ {
		if jstr[i] > ' ' {
			return false
		}
	}
	return true
}

func appendRawPaths(buf []byte, jstr string, paths []pathResult, raw string,
	stringify, del bool,
) ([]byte, error) {
	var err error
	var res gjson.Result
	var found bool
	if del {
		// "-1" 只在数组上表示最后一个元素; 对象中的 "-1" 是普通键, 按键名删除.
		if paths[0].part == "-1" && !paths[0].force &&
			gjson.Parse(jstr).IsArray() {
			res = gjson.Get(jstr, "#")
			if res.Int() > 0 {
				res = gjson.Get(jstr, strconv.FormatInt(int64(res.Int()-1), 10))
				found = true
			}
		}
	}
	// 本地修正: 当前值是字符串时不再向下取值. 上游把字符串的 Raw 交给 gjson, 而 gjson 顶层会跳到第一个
	// '[' 或 '{', 于是 Set({"a":"[1,2]"}, "a.0", "x") 写进字符串内容得到 {"a":"["x",2]"},
	// Delete 则改掉字符串文本却仍是合法 JSON. 字符串按 "未找到" 处理, 与数字等标量及不含括号的字符串一致:
	// Set 替换为新建的对象或数组, Delete 不变. 数字, true/false/null 的 Raw 不含括号, 不受影响.
	if !found && !isStringValue(jstr) {
		res = gjson.Get(jstr, paths[0].gpart)
	}
	if res.Index > 0 {
		if len(paths) > 1 {
			buf = append(buf, jstr[:res.Index]...)
			buf, err = appendRawPaths(buf, res.Raw, paths[1:], raw,
				stringify, del)
			if err != nil {
				return nil, err
			}
			buf = append(buf, jstr[res.Index+len(res.Raw):]...)
			return buf, nil
		}
		buf = append(buf, jstr[:res.Index]...)
		var exidx int // additional forward stripping
		if del {
			var delNextComma bool
			buf, delNextComma = deleteTailItem(buf)
			if delNextComma {
				i, j := res.Index+len(res.Raw), 0
				for ; i < len(jstr); i, j = i+1, j+1 {
					if jstr[i] <= ' ' {
						continue
					}
					if jstr[i] == ',' {
						exidx = j + 1
					}
					break
				}
			}
		} else {
			if stringify {
				buf = appendStringify(buf, raw)
			} else {
				buf = append(buf, raw...)
			}
		}
		buf = append(buf, jstr[res.Index+len(res.Raw)+exidx:]...)
		return buf, nil
	}
	if del {
		return nil, errNoChange
	}
	n, numeric := atoui(paths[0])
	isempty := true
	for i := 0; i < len(jstr); i++ {
		if jstr[i] > ' ' {
			isempty = false
			break
		}
	}
	if isempty {
		if numeric {
			jstr = "[]"
		} else {
			jstr = "{}"
		}
	}
	jsres := gjson.Parse(jstr)
	if jsres.Type != gjson.JSON {
		if numeric {
			jstr = "[]"
		} else {
			jstr = "{}"
		}
		jsres = gjson.Parse(jstr)
	}
	var comma bool
	for i := 1; i < len(jsres.Raw); i++ {
		if jsres.Raw[i] <= ' ' {
			continue
		}
		if jsres.Raw[i] == '}' || jsres.Raw[i] == ']' {
			break
		}
		comma = true
		break
	}
	switch jsres.Raw[0] {
	default:
		return nil, &errorType{"json must be an object or array"}
	case '{':
		end := len(jsres.Raw) - 1
		for ; end > 0; end-- {
			if jsres.Raw[end] == '}' {
				break
			}
		}
		buf = append(buf, jsres.Raw[:end]...)
		if comma {
			buf = append(buf, ',')
		}
		buf, err = appendBuild(buf, false, paths, raw, stringify, 0)
		if err != nil {
			return nil, err
		}
		buf = append(buf, '}')
		return buf, nil
	case '[':
		var appendit bool
		if !numeric {
			if paths[0].part == "-1" && !paths[0].force {
				appendit = true
			} else {
				return nil, &errorType{
					"cannot set array element for non-numeric key '" +
						paths[0].part + "'",
				}
			}
		}
		if appendit {
			njson := trim(jsres.Raw)
			if njson[len(njson)-1] == ']' {
				njson = njson[:len(njson)-1]
			}
			buf = append(buf, njson...)
			if comma {
				buf = append(buf, ',')
			}

			buf, err = appendBuild(buf, true, paths, raw, stringify, 0)
			if err != nil {
				return nil, err
			}
			buf = append(buf, ']')
			return buf, nil
		}
		buf = append(buf, '[')
		ress := jsres.Array()
		for i := range ress {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = append(buf, ress[i].Raw...)
		}
		// 按索引追加时需补齐中间的 null, 超过 maxpad 即拒绝, 避免超大索引放大内存.
		// 一次调用中扩容已有数组最多发生一次, 其后的层级都由 appendBuild 新建, 累计预算从这里传下去.
		grow := n - len(ress)
		if grow > maxpad {
			return nil, makePadError(paths[0].part, len(ress))
		}
		if len(ress) == 0 {
			buf = appendRepeat(buf, "null,", grow)
		} else {
			buf = appendRepeat(buf, ",null", grow)
			if comma {
				buf = append(buf, ',')
			}
		}
		buf, err = appendBuild(buf, true, paths, raw, stringify, max(grow, 0))
		if err != nil {
			return nil, err
		}
		buf = append(buf, ']')
		return buf, nil
	}
}

func isOptimisticPath(path string) bool {
	for i := 0; i < len(path); i++ {
		if path[i] < '.' || path[i] > 'z' {
			return false
		}
		if path[i] > '9' && path[i] < 'A' {
			return false
		}
		if path[i] > 'z' {
			return false
		}
	}
	return true
}

// Set sets a json value for the specified path.
// A path is in dot syntax, such as "name.last" or "age".
// This function expects that the json is well-formed, and does not validate.
// Invalid json will not panic, but it may return back unexpected results.
// An error is returned if the path is not valid.
//
// A path is a series of keys separated by a dot.
//
//	{
//	  "name": {"first": "Tom", "last": "Anderson"},
//	  "age":37,
//	  "children": ["Sara","Alex","Jack"],
//	  "friends": [
//	    {"first": "James", "last": "Murphy"},
//	    {"first": "Roger", "last": "Craig"}
//	  ]
//	}
//	"name.last"          >> "Anderson"
//	"age"                >> 37
//	"children.1"         >> "Alex"
func Set(json, path string, value any) (string, error) {
	return SetOptions(json, path, value, nil)
}

// SetBytes sets a json value for the specified path.
// If working with bytes, this method preferred over
// Set(string(data), path, value)
func SetBytes(json []byte, path string, value any) ([]byte, error) {
	return SetBytesOptions(json, path, value, nil)
}

// SetRaw sets a raw json value for the specified path.
// This function works the same as Set except that the value is set as a
// raw block of json. This allows for setting premarshalled json objects.
func SetRaw(json, path, value string) (string, error) {
	return SetRawOptions(json, path, value, nil)
}

// SetRawOptions sets a raw json value for the specified path with options.
// 与 SetOptions 类似, 但 value 直接作为原始 JSON 写入, 可用于已序列化的 JSON 对象.
func SetRawOptions(json, path, value string, opts *Options) (string, error) {
	var optimistic bool
	if opts != nil {
		optimistic = opts.Optimistic
	}
	res, err := set(json, path, value, false, false, optimistic, false)
	if err == errNoChange {
		return json, nil
	}
	return string(res), err
}

// SetRawBytes sets a raw json value for the specified path.
// If working with bytes, this method preferred over
// SetRaw(string(data), path, value)
func SetRawBytes(json []byte, path string, value []byte) ([]byte, error) {
	return SetRawBytesOptions(json, path, value, nil)
}

type dtype struct{}

// Delete deletes a value from json for the specified path.
func Delete(json, path string) (string, error) {
	return Set(json, path, dtype{})
}

// DeleteOptions deletes a value from json for the specified path, with the given options.
// 路径和错误语义与 Delete 一致; options 可为 nil, 字符串输入不会原地修改.
func DeleteOptions(json, path string, options *Options) (string, error) {
	return SetOptions(json, path, dtype{}, options)
}

// DeleteBytes deletes a value from json for the specified path.
func DeleteBytes(json []byte, path string) ([]byte, error) {
	return SetBytes(json, path, dtype{})
}

// DeleteBytesOptions deletes a value from json for the specified path, with the given options.
// options 可为 nil; 选项沿用 SetBytesOptions 的提示语义, 不保证原地复用输入.
func DeleteBytesOptions(json []byte, path string, options *Options) ([]byte, error) {
	return SetBytesOptions(json, path, dtype{}, options)
}

type stringHeader struct {
	data unsafe.Pointer
	len  int
}

type sliceHeader struct {
	data unsafe.Pointer
	len  int
	cap  int
}

func set(jstr, path, raw string,
	stringify, del, optimistic, inplace bool,
) ([]byte, error) {
	// 空路径表示空字符串键 "", 与 gjson 的寻址一致, 不再作为错误拒绝.
	// 本地修正: 文档本身是字符串时不走乐观路径, 否则 gjson 跳进字符串内容, 原地写坏字符串 (见 appendRawPaths).
	// 嵌套在对象或数组中的字符串 gjson 不会继续取值, 不受影响.
	if !del && optimistic && isOptimisticPath(path) && !isStringValue(jstr) {
		res := gjson.Get(jstr, path)
		if res.Exists() && res.Index > 0 {
			sz := len(jstr) - len(res.Raw) + len(raw)
			if stringify {
				sz += 2
			}
			// 本地修正: 上游在字符串需要转义时直接返回原文且不报错, 修改被静默丢弃.
			// 需要转义时长度无法按原样预估, 改走下方分配路径.
			if inplace && sz <= len(jstr) && (!stringify || !mustMarshalString(raw)) {
				jsonh := *(*stringHeader)(unsafe.Pointer(&jstr))
				jsonbh := sliceHeader{
					data: jsonh.data, len: jsonh.len, cap: jsonh.len,
				}
				jbytes := *(*[]byte)(unsafe.Pointer(&jsonbh))
				if stringify {
					// 本地修正: 先复制值再写开引号. 值可能与输入共享内存 (如取自原值的子切片),
					// 上游先写引号会覆盖值的首字节, 输出 {"a":""23"} 这类非法 JSON 且不报错.
					// copy 按 memmove 处理重叠; 新值不长于原值, 写入范围不会触及其后待移动的尾部.
					copy(jbytes[res.Index+1:], []byte(raw))
					jbytes[res.Index] = '"'
					jbytes[res.Index+1+len(raw)] = '"'
					copy(jbytes[res.Index+1+len(raw)+1:],
						jbytes[res.Index+len(res.Raw):])
				} else {
					copy(jbytes[res.Index:], []byte(raw))
					copy(jbytes[res.Index+len(raw):],
						jbytes[res.Index+len(res.Raw):])
				}
				return jbytes[:sz], nil
			}
			buf := make([]byte, 0, sz)
			buf = append(buf, jstr[:res.Index]...)
			if stringify {
				buf = appendStringify(buf, raw)
			} else {
				buf = append(buf, raw...)
			}
			buf = append(buf, jstr[res.Index+len(res.Raw):]...)
			return buf, nil
		}
	}
	var paths []pathResult
	r, simple := parsePath(path)
	if simple {
		paths = append(paths, r)
		for r.more {
			r, simple = parsePath(r.path)
			if !simple {
				break
			}
			paths = append(paths, r)
		}
	}
	if !simple {
		if del {
			return []byte(jstr),
				&errorType{"cannot delete value from a complex path"}
		}
		return setComplexPath(jstr, path, raw, stringify)
	}
	njson, err := appendRawPaths(nil, jstr, paths, raw, stringify, del)
	if err != nil {
		return []byte(jstr), err
	}
	return njson, nil
}

// setComplexPath 处理含 # 查询等复杂路径: 按 gjson 返回的 Index/Indexes 在原文中替换结果.
//
// 本地修正: 上游直接按偏移切片, 而 gjson 上游的偏移并不总指向原文 (管道后相对子值, # 后接静态值,
// 修饰符或子选择器时为父元素偏移), 会 panic, 拼出非法 JSON, 或在内容恰好相同时改写别的字段
// (如对 {"xy":1,"a":[{"b":1}]} 写 "a|#.b" 会改掉 "xy"). 本地 gjson 已改为按内存同源给出偏移:
// Indexes 要么每项都是元素在原文中的真实位置, 要么为 nil. 这里再做三层兜底:
//   - 每个写入点都须通过 valueAt: 内容一致, 且处在 JSON 值的位置上, 不是数字前缀或键名;
//   - 写入点不在字符串字面量内部 (gjson 会深入字符串内容取值, 偏移真实但不是文档里的值);
//   - 单结果偏移 0 只对 JSON Lines 路径 (以 ".." 开头) 且 Raw 与原文共享起始内存时放行.
//
// 先校验全部写入点再修改, 任一不满足即视为无变化, 与路径不存在时一致, 不会返回改了一半的结果.
func setComplexPath(jstr, path, raw string, stringify bool) ([]byte, error) {
	res := gjson.Get(jstr, path)
	if !res.Exists() {
		return []byte(jstr), errNoChange
	}
	lines := len(path) >= 2 && path[0] == '.' && path[1] == '.'
	if len(res.Indexes) == 0 {
		// 单结果的 Index 由 gjson 按指针计算, 非 0 即为原文中的真实位置. 0 既可能是首行, 也可能是
		// 无法定位的计算结果 (如无匹配时的 "[]" 恰好等于文档 "[]"), 只有 Raw 与原文共享起始内存时才是首行.
		zeroOK := false
		if res.Index == 0 {
			if !lines || len(res.Raw) == 0 ||
				unsafe.StringData(res.Raw) != unsafe.StringData(jstr) {
				return []byte(jstr), errNoChange
			}
			zeroOK = true
		}
		vals := []complexVal{{index: res.Index, raw: res.Raw}}
		if !valueAt(jstr, res.Index, res.Raw, lines, zeroOK) || !outsideStrings(jstr, vals) {
			return []byte(jstr), errNoChange
		}
		return replaceValues(jstr, vals, raw, stringify), nil
	}
	vals := make([]complexVal, 0, len(res.Indexes))
	res.ForEach(func(_, vres gjson.Result) bool {
		vals = append(vals, complexVal{raw: vres.Raw})
		return true
	})
	if len(res.Indexes) != len(vals) {
		return []byte(jstr), errNoChange
	}
	for i := 0; i < len(res.Indexes); i++ {
		vals[i].index = res.Indexes[i]
	}
	sort.SliceStable(vals, func(i, j int) bool {
		return vals[i].index < vals[j].index
	})
	// 按偏移升序逐个确认是原文中的完整值且区间互不重叠. 偏移 0 只可能是 JSON Lines 首行.
	for i, v := range vals {
		if !valueAt(jstr, v.index, v.raw, lines, lines) ||
			(i > 0 && v.index < vals[i-1].index+len(vals[i-1].raw)) {
			return []byte(jstr), errNoChange
		}
	}
	if !outsideStrings(jstr, vals) {
		return []byte(jstr), errNoChange
	}
	return replaceValues(jstr, vals, raw, stringify), nil
}

// outsideStrings 报告各写入点 (按偏移升序) 是否都不在 JSON 字符串字面量内部.
// gjson 会深入字符串内容取值 (对字符串元素继续取值, 顶层 Get 跳到第一个 '[' 或 '{'), 偏移真实存在却落在
// 字符串里, 如 {"a":["[1]"]} 的 "a.#(==\"[1]\").0" 指向 "[1]" 中的 1. valueAt 只看前后一个字节判断不出来,
// 写入会拆开字符串产出非法 JSON. 从文档开头线性扫描到最后一个写入点, 合法 JSON 下判断准确.
//
// 普通字节尽量交给 IndexByte (SIMD) 跳过, 结果与逐字节切换状态 (字符串内遇反斜杠跳过下一字节) 完全相同:
//   - 字符串外逐字节找开引号, 值之间的间隔通常只有一两个字节; 连续 16 个字节都不是引号时 (大数组的数字,
//     缩进空白) 直接跳到下一个引号.
//   - 进入字符串后跳到下一个引号: 其前一字节不是反斜杠即为闭引号, 长文本整段跳过; 否则 (如字符串里嵌着
//     转义的 JSON) 从字符串开头逐字节确定闭引号. 最坏只比逐字节多一次 IndexByte, 总成本线性.
func outsideStrings(jstr string, vals []complexVal) bool {
	i := 0 // 扫描位置, 始终处在字符串外
	for _, v := range vals {
		end := min(v.index, len(jstr))
		run := 0 // 字符串外连续的非引号字节数
		for i < end {
			if jstr[i] != '"' {
				i++
				if run++; run == 16 {
					q := strings.IndexByte(jstr[i:end], '"')
					if q < 0 {
						// 到写入点前都没有引号, 整段都在字符串外: 游标必须推进到 end,
						// 否则下一个写入点会从这里重新扫描, 多写入点时退化为二次复杂度.
						i = end
						break
					}
					i += q
				}
				continue
			}
			run = 0
			j := i + 1
			e := strings.IndexByte(jstr[j:], '"')
			if e < 0 {
				// 字符串直到文档末尾都未闭合, 写入点在其内部.
				return false
			}
			j += e
			if jstr[j-1] == '\\' {
				// 引号可能被转义: 从字符串开头逐字节确定真正的闭引号.
				for j = i + 1; ; j++ {
					if j >= len(jstr) {
						return false
					}
					if c := jstr[j]; c == '\\' {
						j++
					} else if c == '"' {
						break
					}
				}
			}
			if j >= end {
				// 闭引号在写入点处或之后: 写入点落在这个字符串里.
				return false
			}
			i = j + 1
		}
	}
	return true
}

// complexVal 是复杂路径的一个写入点: 原文偏移和该处原值.
type complexVal struct {
	index int
	raw   string
}

// replaceValues 把 jstr 中各写入点的原值替换为 raw (stringify 时写成 JSON 字符串), 一次分配构建结果.
// vals 须已按偏移升序且区间互不重叠. 上游每个写入点都复制整个文档并在 string/[]byte 间往返转换.
func replaceValues(jstr string, vals []complexVal, raw string, stringify bool) []byte {
	vlen := len(raw)
	if stringify {
		// 只算引号; 需要转义时 appendStringify 自行扩容.
		vlen += 2
	}
	n := len(jstr)
	for _, v := range vals {
		n += vlen - len(v.raw)
	}
	buf := make([]byte, 0, n)
	prev := 0
	for _, v := range vals {
		buf = append(buf, jstr[prev:v.index]...)
		if stringify {
			buf = appendStringify(buf, raw)
		} else {
			buf = append(buf, raw...)
		}
		prev = v.index + len(v.raw)
	}
	return append(buf, jstr[prev:]...)
}

// valueAt 报告 jstr[index:index+len(raw)] 是否就是原文中的一个完整 JSON 值:
// 内容与 raw 一致, 前一个非空白字节是 '[', ',', ':' 或文档开头, 后一个非空白字节是 ',', ']', '}'
// 或文档结尾. 这排除了数字前缀 ("1" 落在 "12" 上), 键名位置和拼接处等内容碰巧相同的偏移.
// lines 为 JSON Lines 路径时, 中间隔着换行的相邻行也算边界; zeroOK 为 false 时拒绝偏移 0.
func valueAt(jstr string, index int, raw string, lines, zeroOK bool) bool {
	if index < 0 || (index == 0 && !zeroOK) || len(raw) == 0 ||
		index+len(raw) > len(jstr) || jstr[index:index+len(raw)] != raw {
		return false
	}
	nl := false
	i := index - 1
	for ; i >= 0 && jstr[i] <= ' '; i-- {
		nl = nl || jstr[i] == '\n'
	}
	if i >= 0 && jstr[i] != '[' && jstr[i] != ',' && jstr[i] != ':' && !(lines && nl) {
		return false
	}
	nl = false
	i = index + len(raw)
	for ; i < len(jstr) && jstr[i] <= ' '; i++ {
		nl = nl || jstr[i] == '\n'
	}
	return i == len(jstr) || jstr[i] == ',' || jstr[i] == ']' || jstr[i] == '}' || (lines && nl)
}

// SetOptions sets a json value for the specified path with options.
// A path is in dot syntax, such as "name.last" or "age".
// This function expects that the json is well-formed, and does not validate.
// Invalid json will not panic, but it may return back unexpected results.
// An error is returned if the path is not valid.
func SetOptions(json, path string, value any,
	opts *Options,
) (string, error) {
	if opts != nil {
		if opts.ReplaceInPlace {
			// it's not safe to replace bytes in-place for strings
			// copy the Options and set options.ReplaceInPlace to false.
			nopts := *opts
			opts = &nopts
			opts.ReplaceInPlace = false
		}
	}
	jsonh := *(*stringHeader)(unsafe.Pointer(&json))
	jsonbh := sliceHeader{data: jsonh.data, len: jsonh.len, cap: jsonh.len}
	jsonb := *(*[]byte)(unsafe.Pointer(&jsonbh))
	res, err := SetBytesOptions(jsonb, path, value, opts)
	return string(res), err
}

// SetBytesOptions sets a json value for the specified path with options.
// If working with bytes, this method preferred over
// SetOptions(string(data), path, value)
func SetBytesOptions(json []byte, path string, value any,
	opts *Options,
) ([]byte, error) {
	var optimistic, inplace bool
	if opts != nil {
		optimistic = opts.Optimistic
		inplace = opts.ReplaceInPlace
	}
	jstr := *(*string)(unsafe.Pointer(&json))
	var res []byte
	var err error
tryf64:
	switch v := value.(type) {
	default:
		b, merr := jsongo.Marshal(value)
		if merr != nil {
			return nil, merr
		}
		raw := *(*string)(unsafe.Pointer(&b))
		res, err = set(jstr, path, raw, false, false, optimistic, inplace)
	case dtype:
		res, err = set(jstr, path, "", false, true, optimistic, inplace)
	case string:
		res, err = set(jstr, path, v, true, false, optimistic, inplace)
	case []byte:
		raw := *(*string)(unsafe.Pointer(&v))
		res, err = set(jstr, path, raw, true, false, optimistic, inplace)
	case bool:
		if v {
			res, err = set(jstr, path, "true", false, false, optimistic, inplace)
		} else {
			res, err = set(jstr, path, "false", false, false, optimistic, inplace)
		}
	case int8:
		res, err = set(jstr, path, strconv.FormatInt(int64(v), 10),
			false, false, optimistic, inplace)
	case int16:
		res, err = set(jstr, path, strconv.FormatInt(int64(v), 10),
			false, false, optimistic, inplace)
	case int32:
		res, err = set(jstr, path, strconv.FormatInt(int64(v), 10),
			false, false, optimistic, inplace)
	case int64:
		res, err = set(jstr, path, strconv.FormatInt(int64(v), 10),
			false, false, optimistic, inplace)
	case uint8:
		res, err = set(jstr, path, strconv.FormatUint(uint64(v), 10),
			false, false, optimistic, inplace)
	case uint16:
		res, err = set(jstr, path, strconv.FormatUint(uint64(v), 10),
			false, false, optimistic, inplace)
	case uint32:
		res, err = set(jstr, path, strconv.FormatUint(uint64(v), 10),
			false, false, optimistic, inplace)
	case uint64:
		res, err = set(jstr, path, strconv.FormatUint(uint64(v), 10),
			false, false, optimistic, inplace)
	case float32:
		// 转为 float64 后复用同一套格式化与非有限值检查.
		value = float64(v)
		goto tryf64
	case float64:
		s := strconv.FormatFloat(v, 'f', -1, 64)
		// NaN/±Inf 不是合法 JSON 数字: 写入带引号的占位字符串保证结果仍是合法 JSON,
		// 同时返回 encoding/json 的 UnsupportedValueError, 由调用方决定是否采用该结果.
		if s[0] == 'N' || s[0] == 'I' || (s[0] == '-' && s[1] == 'I') ||
			(s[0] == '+' && s[1] == 'I') {
			s = strconv.Quote(s)
			res, err = set(jstr, path, s, false, false, optimistic, inplace)
			if err == nil {
				_, err = jsongo.Marshal(v)
			}
		} else {
			res, err = set(jstr, path, s, false, false, optimistic, inplace)
		}
	}
	if err == errNoChange {
		return json, nil
	}
	return res, err
}

// SetRawBytesOptions sets a raw json value for the specified path with options.
// If working with bytes, this method preferred over
// SetRawOptions(string(data), path, value, opts)
func SetRawBytesOptions(json []byte, path string, value []byte,
	opts *Options,
) ([]byte, error) {
	jstr := *(*string)(unsafe.Pointer(&json))
	vstr := *(*string)(unsafe.Pointer(&value))
	var optimistic, inplace bool
	if opts != nil {
		optimistic = opts.Optimistic
		inplace = opts.ReplaceInPlace
	}
	res, err := set(jstr, path, vstr, false, false, optimistic, inplace)
	if err == errNoChange {
		return json, nil
	}
	return res, err
}
