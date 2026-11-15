package orderedmap

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

type PairOf[K comparable, V any] struct {
	key   K
	value V
}

func (kv *PairOf[K, V]) Key() K {
	return kv.key
}

func (kv *PairOf[K, V]) Value() V {
	return kv.value
}

type ByPairOf[K comparable, V any] struct {
	Pairs    []*PairOf[K, V]
	LessFunc func(a *PairOf[K, V], j *PairOf[K, V]) bool
}

func (a ByPairOf[K, V]) Len() int           { return len(a.Pairs) }
func (a ByPairOf[K, V]) Swap(i, j int)      { a.Pairs[i], a.Pairs[j] = a.Pairs[j], a.Pairs[i] }
func (a ByPairOf[K, V]) Less(i, j int) bool { return a.LessFunc(a.Pairs[i], a.Pairs[j]) }

type OrderedMapOf[K comparable, V any] struct {
	keys       []K
	values     map[K]V
	escapeHTML bool
}

func NewOf[K comparable, V any]() *OrderedMapOf[K, V] {
	o := OrderedMapOf[K, V]{}
	o.keys = []K{}
	o.values = map[K]V{}
	o.escapeHTML = true
	return &o
}

func (o *OrderedMapOf[K, V]) SetEscapeHTML(on bool) {
	o.escapeHTML = on
}

func (o *OrderedMapOf[K, V]) Get(key K) (V, bool) {
	val, exists := o.values[key]
	return val, exists
}

func (o *OrderedMapOf[K, V]) MustGet(key K) V {
	val, _ := o.values[key]
	return val
}

func (o *OrderedMapOf[K, V]) Set(key K, value V) {
	_, exists := o.values[key]
	if !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *OrderedMapOf[K, V]) Delete(key K) {
	// check key is in use
	_, ok := o.values[key]
	if !ok {
		return
	}
	// remove from keys
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	// remove from values
	delete(o.values, key)
}

func (o *OrderedMapOf[K, V]) Size() int {
	return len(o.keys)
}

func (o *OrderedMapOf[K, V]) Keys() []K {
	return o.keys
}

func (o *OrderedMapOf[K, V]) Values() map[K]V {
	return o.values
}

// SortKeys Sort the map keys using your sort func
func (o *OrderedMapOf[K, V]) SortKeys(sortFunc func(keys []K)) {
	sortFunc(o.keys)
}

// Sort the map using your sort func
func (o *OrderedMapOf[K, V]) Sort(lessFunc func(a *PairOf[K, V], b *PairOf[K, V]) bool) {
	pairs := make([]*PairOf[K, V], len(o.keys))
	for i, key := range o.keys {
		pairs[i] = &PairOf[K, V]{key, o.values[key]}
	}

	sort.Sort(ByPairOf[K, V]{pairs, lessFunc})

	for i, pair := range pairs {
		o.keys[i] = pair.key
	}
}

// MarshalJSON 按插入 (或排序后) 的键顺序输出 JSON 对象, 键按 encoding/json 的 map 键规则转为字符串 (见 writeKeyOf).
// 键类型无法表示为 JSON 字符串时返回错误, 不输出非法对象.
func (o *OrderedMapOf[K, V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(o.escapeHTML)
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		// add key
		if err := writeKeyOf(&buf, encoder, k); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		// add value
		if err := encoder.Encode(o.values[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// writeKeyOf 把 k 编码为 JSON 对象键 (带引号的字符串) 追加到 buf, encoder 须写入 buf 且已设置 escapeHTML.
// 先走旧版编码路径 (encoder.Encode(k)), 成功且结果是 JSON 字符串即采用, 旧版已合法的输出逐字节不变
// (含 MarshalJSON 返回字符串的键, 这一点与 encoding/json 忽略键的 MarshalJSON 不同, 为兼容保留).
// 旧路径出错 (如非法 json.Number) 或结果不是 JSON 字符串 (如数字) 时, 按 encoding/json 的 map 键规则回退:
//   - 底层为字符串的类型: 取其字符串值;
//   - 实现 encoding.TextMarshaler: 取 MarshalText 结果, nil 指针为空串;
//   - 整数/无符号整数类型 (含底层为整数的自定义类型): 十进制字符串;
//   - 其他类型 (bool, 浮点, 普通结构体, nil 接口等) 返回错误, encoding/json 对这类 map 键同样报错.
//
// K 为接口类型时按动态值判断. 旧版直接编码键, 整数键会生成 {42:"v"} 这类非法对象且不报错.
func writeKeyOf[K comparable](buf *bytes.Buffer, encoder *json.Encoder, k K) error {
	kv := any(k)
	// 最常见的 string 键直接编码, 不走反射; 复用已装箱的 kv, 避免再次装箱分配.
	if _, ok := kv.(string); ok {
		return encoder.Encode(kv)
	}
	start := buf.Len()
	// Encoder 只在编码成功后整体写出, 出错时 buf 不含残留; 成功但不是字符串时撤销本次输出.
	if err := encoder.Encode(kv); err == nil {
		if b := buf.Bytes(); len(b) > start && b[start] == '"' {
			return nil
		}
		buf.Truncate(start)
	}

	rv := reflect.ValueOf(kv)
	if rv.Kind() == reflect.String {
		return encoder.Encode(rv.String())
	}
	if tm, ok := kv.(encoding.TextMarshaler); ok {
		if rv.Kind() == reflect.Pointer && rv.IsNil() {
			return encoder.Encode("")
		}
		text, err := tm.MarshalText()
		if err != nil {
			return fmt.Errorf("orderedmap: marshal text key of type %T: %w", kv, err)
		}
		return encoder.Encode(string(text))
	}
	switch {
	case rv.CanInt():
		return encoder.Encode(strconv.FormatInt(rv.Int(), 10))
	case rv.CanUint():
		return encoder.Encode(strconv.FormatUint(rv.Uint(), 10))
	default:
		return fmt.Errorf("orderedmap: unsupported key type %T, key must marshal to a JSON string", kv)
	}
}
