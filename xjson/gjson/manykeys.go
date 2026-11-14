package gjson

import (
	"math/bits"
	"strconv"
	"unsafe"
)

// manyKeysMax 是单遍扫描支持的请求键数量上限, 由 uint64 命中位图宽度决定; 超出时逐键回退到 Get.
const manyKeysMax = 64

// GetManyKeys 一次扫描顶层 JSON 对象, 取出多个顶层字面量键的值. 本函数是本地扩展, 上游没有对应 API.
//
// keys 中的每个键按字面量匹配, 不解析路径语法: 键 "a.b" 只匹配名为 "a.b" 的顶层键, 不进入嵌套对象.
// 第 i 个结果与 Get(json, Escape(keys[i])) 逐字段一致 (Type/Raw/Str/Num/Index/Indexes):
//   - JSON 中重复的键取首次出现的值; keys 内的重复键共享同一结果.
//   - JSON 键含转义时先反转义再比较, 与 Get 一致.
//   - 与 Get 一样不校验 JSON 合法性; 畸形输入不会 panic, 结果与逐键 Get 相同.
//
// 与对每个键分别调用 Get 相比, 本函数只扫描一次, 全部命中后提前结束; 有键不存在时扫到对象末尾.
// 只查单个键时没有收益, 应直接用 Get.
//
// 返回值长度等于 len(keys). cap(dst) >= len(keys) 时复用 dst 的底层数组并先清零, 否则新分配.
// 结果与 Get 一样引用 json 的内存. 调用方用 unsafe 方式把 []byte 转成 json 时 (如 utils.B2S),
// 结果用完之前该字节切片不得被修改或归还缓冲池, 结果也不得交给异步消费者或长期保存.
//
// 以下情况逐键回退到 Get(json, Escape(k)), 结果仍一致, 但不再是单遍扫描, Escape 也可能分配:
//   - 键数超过 64.
//   - 存在空键或含 '*'、'?' 的键 (保守处理, 不在扫描中复刻路径通配的边界).
//   - 顶层第一个容器是数组 (Get 会把数字键解释为数组下标).
//
// 以下情况即使走单遍扫描也会分配: dst 容量不足, 命中值含转义 (Str 需要反转义), JSON 键含转义 (比较前反转义).
func GetManyKeys(json string, keys []string, dst []Result) []Result {
	n := len(keys)
	if cap(dst) < n {
		dst = make([]Result, n)
	} else {
		dst = dst[:n]
		clear(dst)
	}
	if n == 0 {
		return dst
	}
	if n > manyKeysMax {
		return manyKeysFallback(json, keys, dst)
	}
	for _, k := range keys {
		if !manyKeysScannable(k) {
			return manyKeysFallback(json, keys, dst)
		}
	}
	// 顶层定位与 Get 相同: 跳过任意前导字节, 直到第一个 '{' 或 '['; 两者都没有时结果全部不存在.
	for i := 0; i < len(json); i++ {
		switch json[i] {
		case '{':
			manyKeysScanObject(json, i+1, keys, dst)
			return dst
		case '[':
			return manyKeysFallback(json, keys, dst)
		}
	}
	return dst
}

// manyKeysScannable 报告键能否走单遍扫描.
//
// 空键和含通配符的键交给 Get 处理: 它们在路径解析中有额外分支, 回退可以直接保证等价, 无需在扫描中复刻.
func manyKeysScannable(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] == '*' || k[i] == '?' {
			return false
		}
	}
	return true
}

// manyKeysFallback 逐键调用 Get(json, Escape(k)), 是单遍扫描的等价对照.
//
// dst 已由调用方按 len(keys) 截取并清零.
func manyKeysFallback(json string, keys []string, dst []Result) []Result {
	for i, k := range keys {
		dst[i] = Get(json, Escape(k))
	}
	return dst
}

// manyKeysScanObject 从顶层对象 '{' 之后的位置 i 开始扫描, 把命中的值写入 dst.
//
// 键与值的消费逐字节复刻 parseObject (路径不含 '.'、'|' 且非通配时的分支): 值的消费方式与是否命中无关,
// 因此对任意输入 (含畸形输入), 扫描到每个键时的位置和状态都与单独调用 Get 相同.
// 唯一区别是命中后记录结果并继续扫描, 直到全部命中或对象结束.
// 同步上游 parseObject 的改动时必须同时复核本函数及 manyKeysParseValue.
// 键解析与未命中分支保持与上游逐行可对照; 命中分支单独成函数, 减少热循环中的活跃变量和分支.
// 实测这一拆分使单键或命中靠前时的扫描成本回到与 Get 持平 (拆分前约慢 20%).
func manyKeysScanObject(json string, i int, keys []string, dst []Result) {
	var kesc, ok bool
	var key string
	// done 标记已命中的请求键; all 是全部请求键都命中时的位图.
	var done uint64
	all := ^uint64(0) >> (manyKeysMax - len(keys))
	// sigs 是全部请求键指纹的位集合. 绝大多数 JSON 键的指纹不在集合内, 可跳过逐个比较请求键,
	// 使每个 JSON 键的额外开销接近 parseObject 的单次比较. 指纹只做预过滤, 命中后仍逐字节比较.
	var sigs uint64
	for _, k := range keys {
		sigs |= 1 << manyKeysSig(k)
	}
	for i < len(json) {
		// 解析键字符串, 与 parseObject 的 parse_key_string 段相同.
		for ; i < len(json); i++ {
			if json[i] == '"' {
				i++
				s := i
				for ; i < len(json); i++ {
					if json[i] > '\\' {
						continue
					}
					if json[i] == '"' {
						i, key, kesc, ok = i+1, json[s:i], false, true
						goto keyDone
					}
					if json[i] == '\\' {
						i++
						for ; i < len(json); i++ {
							if json[i] > '\\' {
								continue
							}
							if json[i] == '"' {
								// 引号前有反斜杠时, 按连续反斜杠的奇偶判断引号是否被转义; j > 0 与上游一致.
								if json[i-1] == '\\' {
									n := 0
									for j := i - 2; j > 0; j-- {
										if json[j] != '\\' {
											break
										}
										n++
									}
									if n%2 == 0 {
										continue
									}
								}
								i, key, kesc, ok = i+1, json[s:i], true, true
								goto keyDone
							}
						}
						break
					}
				}
				key, kesc, ok = json[s:], false, false
			keyDone:
				break
			}
			if json[i] == '}' {
				return
			}
		}
		if !ok {
			return
		}

		// 同一个 JSON 键同时命中所有尚未命中的同名请求键, 对应 Get 的 "重复键取首个".
		// 已命中的请求键不再参与比较, 因此 JSON 中后出现的重复键不会覆盖首个值.
		// 请求键均非空 (空键已回退), 因此空 JSON 键不可能命中, 也无需计算指纹.
		if kesc {
			key = unescape(key)
		}
		if key != "" && sigs&(1<<manyKeysSig(key)) != 0 {
			if hit := manyKeysMatch(keys, key, done); hit != 0 {
				// 命中路径 (每个请求键至多一次) 放在独立函数, 让下面的未命中路径保持与 parseObject 同样精简.
				var r Result
				i, r, ok = manyKeysParseValue(json, i)
				if !ok {
					// 与 parseObject 命中分支一致: 没有可用的值 (到达末尾或字符串未闭合) 即结束, 不记录结果.
					return
				}
				manyKeysFillIndex(json, &r)
				for rest := hit; rest != 0; rest &= rest - 1 {
					dst[bits.TrailingZeros64(rest)] = r
				}
				done |= hit
				if done == all {
					return
				}
				continue
			}
		}

		// 未命中: 与 parseObject 未命中分支相同, 按首字符分派消费值, 不构造结果.
		for ; i < len(json); i++ {
			var num bool
			switch json[i] {
			default:
				continue
			case '"':
				i++
				i, _, _, ok = parseString(json, i)
				if !ok {
					return
				}
			case '{', '[':
				i, _ = parseSquash(json, i)
			case 'n':
				// 'n' 后不是 'u' 时按数字处理 (兼容 NaN 之类的写法), 与 parseObject 相同.
				if i+1 < len(json) && json[i+1] != 'u' {
					num = true
					break
				}
				fallthrough
			case 't', 'f':
				i, _ = parseLiteral(json, i)
			case '+', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
				'i', 'I', 'N':
				num = true
			}
			if num {
				i, _ = parseNumber(json, i)
			}
			break
		}
	}
}

// manyKeysMatch 返回尚未命中且与 key 相等的请求键位图; 调用方已用指纹预过滤.
func manyKeysMatch(keys []string, key string, done uint64) uint64 {
	var hit uint64
	for j := range keys {
		if done&(1<<uint(j)) == 0 && keys[j] == key {
			hit |= 1 << uint(j)
		}
	}
	return hit
}

// manyKeysParseValue 从键之后的位置 i 起消费一个值并构造结果, 分派与赋值方式与 parseObject 命中分支相同.
//
// 返回消费后的位置、结果和是否取得值. 跳过值之前的任意字节 (通常是冒号和空白); 到达末尾或字符串未闭合时返回 false.
// 结果的 Index 由调用方按 fillIndex 规则补齐.
func manyKeysParseValue(json string, i int) (int, Result, bool) {
	var r Result
	for ; i < len(json); i++ {
		switch json[i] {
		default:
			continue
		case '"':
			var val string
			var vesc, ok bool
			i, val, vesc, ok = parseString(json, i+1)
			if !ok {
				return i, r, false
			}
			if vesc {
				r.Str = unescape(val[1 : len(val)-1])
			} else {
				r.Str = val[1 : len(val)-1]
			}
			r.Raw = val
			r.Type = String
			return i, r, true
		case '{', '[':
			i, r.Raw = parseSquash(json, i)
			r.Type = JSON
			return i, r, true
		case 'n':
			// 'n' 后不是 'u' 时按数字处理 (兼容 NaN 之类的写法), 与 parseObject 相同.
			if i+1 < len(json) && json[i+1] != 'u' {
				return manyKeysParseNumber(json, i)
			}
			fallthrough
		case 't', 'f':
			vc := json[i]
			i, r.Raw = parseLiteral(json, i)
			switch vc {
			case 't':
				r.Type = True
			case 'f':
				r.Type = False
			}
			return i, r, true
		case '+', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
			'i', 'I', 'N':
			return manyKeysParseNumber(json, i)
		}
	}
	return i, r, false
}

// manyKeysParseNumber 消费数字 (含 NaN/Inf 写法) 并构造结果, 与 parseObject 的数字分支相同.
func manyKeysParseNumber(json string, i int) (int, Result, bool) {
	var r Result
	i, r.Raw = parseNumber(json, i)
	r.Type = Number
	r.Num, _ = strconv.ParseFloat(r.Raw, 64)
	return i, r, true
}

// manyKeysSig 计算非空键的 6 位指纹 (0~63), 由长度和首尾字节组合而成, 用于预过滤不可能命中的键.
//
// 相同的键指纹必然相同; 不同的键可能碰撞, 碰撞只会多做一次逐字节比较, 不影响结果. 调用方保证 k 非空.
func manyKeysSig(k string) uint {
	return (uint(len(k)) + uint(k[0])*3 + uint(k[len(k)-1])*5) & 63
}

// manyKeysFillIndex 与 fillIndex 规则相同: Index 是 Raw 在 json 中的字节偏移, 越界视为未知 (0).
//
// 单遍扫描的结果都是 json 的子串, 不存在 fillIndex 中 calcd 计算值的情况.
func manyKeysFillIndex(json string, r *Result) {
	if len(r.Raw) == 0 {
		return
	}
	r.Index = int(uintptr(unsafe.Pointer(unsafe.StringData(r.Raw))) - uintptr(unsafe.Pointer(unsafe.StringData(json))))
	if r.Index < 0 || r.Index >= len(json) {
		r.Index = 0
	}
}
