package orderedmap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestOrderedMapOf(t *testing.T) {
	o := NewOf[string, any]()
	// number
	o.Set("number", 3)
	v, _ := o.Get("number")
	if v.(int) != 3 {
		t.Error("Set number")
	}
	// string
	o.Set("string", "x")
	v, _ = o.Get("string")
	if v.(string) != "x" {
		t.Error("Set string")
	}
	// string slice
	o.Set("strings", []string{
		"t",
		"u",
	})
	v, _ = o.Get("strings")
	if v.([]string)[0] != "t" {
		t.Error("Set strings first index")
	}
	if v.([]string)[1] != "u" {
		t.Error("Set strings second index")
	}
	// mixed slice
	o.Set("mixed", []any{
		1,
		"1",
	})
	v, _ = o.Get("mixed")
	if v.([]any)[0].(int) != 1 {
		t.Error("Set mixed int")
	}
	if v.([]any)[1].(string) != "1" {
		t.Error("Set mixed string")
	}
	// overriding existing key
	o.Set("number", 4)
	v, _ = o.Get("number")
	if v.(int) != 4 {
		t.Error("Override existing key")
	}
	// Keys method
	keys := o.Keys()
	expectedKeys := []string{
		"number",
		"string",
		"strings",
		"mixed",
	}
	for i, key := range keys {
		if key != expectedKeys[i] {
			t.Error("Keys method", key, "!=", expectedKeys[i])
		}
	}
	// Values method
	values := o.Values()
	expectedValues := map[string]any{
		"number":  4,
		"string":  "x",
		"strings": []string{"t", "u"},
		"mixed":   []any{1, "1"},
	}
	if !reflect.DeepEqual(values, expectedValues) {
		t.Error("Values method returned unexpected map")
	}
	// delete
	o.Delete("strings")
	o.Delete("not a key being used")
	if len(o.Keys()) != 3 {
		t.Error("Delete method")
	}
	_, ok := o.Get("strings")
	if ok {
		t.Error("Delete did not remove 'strings' key")
	}
}

func TestOrderedMapOf_SortKeys(t *testing.T) {
	o := NewOf[string, int]()
	o.Set("b", 2)
	o.Set("a", 1)
	o.Set("c", 3)

	o.SortKeys(sort.Strings)

	// Check the root keys
	expectedKeys := []string{
		"a",
		"b",
		"c",
	}
	k := o.Keys()
	for i := range k {
		if k[i] != expectedKeys[i] {
			t.Error("SortKeys root key order", i, k[i], "!=", expectedKeys[i])
		}
	}
}

func TestOrderedMapOf_Sort(t *testing.T) {
	o := NewOf[string, float64]()
	o.Set("b", 2.1)
	o.Set("a", 1.1)
	o.Set("c", 3)

	o.Sort(func(a *PairOf[string, float64], b *PairOf[string, float64]) bool {
		return a.value > b.value
	})

	// Check the root keys
	expectedKeys := []string{
		"c",
		"b",
		"a",
	}
	k := o.Keys()
	for i := range k {
		if k[i] != expectedKeys[i] {
			t.Error("Sort root key order", i, k[i], "!=", expectedKeys[i])
		}
	}
}

func TestOrderedMapOf_BlankMarshalJSON(t *testing.T) {
	o := NewOf[int, int]()
	// blank map
	b, err := json.Marshal(o)
	if err != nil {
		t.Error("Marshalling blank map to json", err)
	}
	s := string(b)
	// check json is correctly ordered
	if s != `{}` {
		t.Error("JSON Marshaling blank map value is incorrect", s)
	}
	// convert to indented json
	bi, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		t.Error("Marshalling indented json for blank map", err)
	}
	si := string(bi)
	ei := `{}`
	if si != ei {
		fmt.Println(ei)
		fmt.Println(si)
		t.Error("JSON MarshalIndent blank map value is incorrect", si)
	}
}

func TestOrderedMapOf_MarshalJSON(t *testing.T) {
	o := NewOf[string, any]()
	// number
	o.Set("number", 3)
	// string
	o.Set("string", "x")
	// string
	o.Set("specialstring", "\\.<>[]{}_-")
	// new value keeps key in old position
	o.Set("number", 4)
	// keys not sorted alphabetically
	o.Set("z", 1)
	o.Set("a", 2)
	o.Set("b", 3)
	// slice
	o.Set("slice", []any{
		"1",
		1,
	})
	// orderedmap
	v := New()
	v.Set("e", 1)
	v.Set("a", 2)
	o.Set("orderedmap", v)
	// escape key
	o.Set("test\n\r\t\\\"ing", 9)
	// convert to json
	b, err := json.Marshal(o)
	if err != nil {
		t.Error("Marshalling json", err)
	}
	s := string(b)
	// check json is correctly ordered
	if s != `{"number":4,"string":"x","specialstring":"\\.\u003c\u003e[]{}_-","z":1,"a":2,"b":3,"slice":["1",1],"orderedmap":{"e":1,"a":2},"test\n\r\t\\\"ing":9}` {
		t.Error("JSON Marshal value is incorrect", s)
	}
	// convert to indented json
	bi, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		t.Error("Marshalling indented json", err)
	}
	si := string(bi)
	ei := `{
  "number": 4,
  "string": "x",
  "specialstring": "\\.\u003c\u003e[]{}_-",
  "z": 1,
  "a": 2,
  "b": 3,
  "slice": [
    "1",
    1
  ],
  "orderedmap": {
    "e": 1,
    "a": 2
  },
  "test\n\r\t\\\"ing": 9
}`
	if si != ei {
		fmt.Println(ei)
		fmt.Println(si)
		t.Error("JSON MarshalIndent value is incorrect", si)
	}
}

func TestOrderedMapOf_MarshalJSONNoEscapeHTML(t *testing.T) {
	o := NewOf[string, string]()
	o.SetEscapeHTML(false)
	// string special characters
	o.Set("specialstring", "\\.<>[]{}_-")
	// convert to json
	b, err := o.MarshalJSON()
	if err != nil {
		t.Error("Marshalling json", err)
	}
	s := strings.Replace(string(b), "\n", "", -1)
	// check json is correctly ordered
	if s != `{"specialstring":"\\.<>[]{}_-"}` {
		t.Error("JSON Marshal value is incorrect", s)
	}
}

type (
	keyLevel   int16  // 底层为整数的自定义键
	keyName    string // 底层为字符串的自定义键
	keyTxtInt  int    // 整数键但实现 TextMarshaler, 按 encoding/json 规则用 MarshalText
	keyJSONStr int    // 整数键但实现 json.Marshaler 且产出 JSON 字符串, 保持旧版输出
	keyStruct  struct{ A int }
	keyDual    int // 同时实现两个接口, MarshalJSON 产出数字: 回退用 MarshalText (同 encoding/json)
	keyBadJSON int // MarshalJSON 报错且无 TextMarshaler: 回退按整数 (encoding/json 忽略键的 MarshalJSON)
	keyPtrTxt  struct{ S string }
)

func (keyDual) MarshalJSON() ([]byte, error) { return []byte("7"), nil }

func (keyDual) MarshalText() ([]byte, error) { return []byte("seven"), nil }

func (keyBadJSON) MarshalJSON() ([]byte, error) { return nil, errors.New("fixture key error") }

func (k *keyPtrTxt) MarshalText() ([]byte, error) { return []byte("P" + k.S), nil }

func (k keyTxtInt) MarshalText() ([]byte, error) { return []byte(fmt.Sprintf("L%d", int(k))), nil }

func (k keyJSONStr) MarshalJSON() ([]byte, error) { return []byte(fmt.Sprintf(`"J%d"`, int(k))), nil }

// marshalOf 返回 o.MarshalJSON 的输出压缩后的文本, 同时校验其为合法 JSON 对象.
func marshalOf(t *testing.T, o json.Marshaler) string {
	t.Helper()
	b, err := o.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("MarshalJSON produced invalid object %q: %v", b, err)
	}
	return strings.ReplaceAll(string(b), "\n", "")
}

// TestOrderedMapOf_MarshalJSONKeyTypes 验证非字符串键按 encoding/json 的 map 键规则输出合法对象键:
// 整数类转十进制字符串, TextMarshaler 用其文本, 已能产出 JSON 字符串的旧输出保持不变, 不支持的类型返回错误.
func TestOrderedMapOf_MarshalJSONKeyTypes(t *testing.T) {
	ints := NewOf[int, string]()
	ints.Set(42, "value")
	ints.Set(-1, "neg")
	if got, want := marshalOf(t, ints), `{"42":"value","-1":"neg"}`; got != want {
		t.Errorf("int keys = %s, want %s", got, want)
	}
	if b, err := json.Marshal(ints); err != nil || string(b) != `{"42":"value","-1":"neg"}` {
		t.Errorf("json.Marshal int keys = %s, %v", b, err)
	}

	u8 := NewOf[uint8, int]()
	u8.Set(255, 1)
	if got, want := marshalOf(t, u8), `{"255":1}`; got != want {
		t.Errorf("uint8 keys = %s, want %s", got, want)
	}

	lv := NewOf[keyLevel, int]()
	lv.Set(-7, 1)
	if got, want := marshalOf(t, lv), `{"-7":1}`; got != want {
		t.Errorf("custom int keys = %s, want %s", got, want)
	}

	txt := NewOf[keyTxtInt, int]()
	txt.Set(3, 1)
	if got, want := marshalOf(t, txt), `{"L3":1}`; got != want {
		t.Errorf("TextMarshaler int keys = %s, want %s", got, want)
	}

	js := NewOf[keyJSONStr, int]()
	js.Set(5, 1)
	if got, want := marshalOf(t, js), `{"J5":1}`; got != want {
		t.Errorf("json.Marshaler string keys = %s, want %s", got, want)
	}

	addr := NewOf[netip.Addr, int]()
	addr.Set(netip.MustParseAddr("1.2.3.4"), 1)
	if got, want := marshalOf(t, addr), `{"1.2.3.4":1}`; got != want {
		t.Errorf("netip.Addr keys = %s, want %s", got, want)
	}

	num := NewOf[json.Number, int]()
	num.Set("42", 1)
	if got, want := marshalOf(t, num), `{"42":1}`; got != want {
		t.Errorf("json.Number keys = %s, want %s", got, want)
	}

	anyKey := NewOf[any, int]()
	anyKey.Set(1, 1)
	anyKey.Set("a", 2)
	if got, want := marshalOf(t, anyKey), `{"1":1,"a":2}`; got != want {
		t.Errorf("any keys = %s, want %s", got, want)
	}

	// 旧路径出错或产出非字符串时, 回退结果与 encoding/json 的 map 键一致.
	assertStdKey := func(name string, o json.Marshaler, std any, want string) {
		t.Helper()
		if got := marshalOf(t, o); got != want {
			t.Errorf("%s keys = %s, want %s", name, got, want)
		}
		if b, err := json.Marshal(std); err != nil || string(b) != want {
			t.Errorf("%s: encoding/json = %s, %v; fixture expectation %s is wrong", name, b, err, want)
		}
	}
	dual := NewOf[keyDual, int]()
	dual.Set(1, 1)
	assertStdKey("dual-interface", dual, map[keyDual]int{1: 1}, `{"seven":1}`)
	bad := NewOf[keyBadJSON, int]()
	bad.Set(9, 1)
	assertStdKey("failing MarshalJSON", bad, map[keyBadJSON]int{9: 1}, `{"9":1}`)
	for _, n := range []json.Number{"abc", "1e3", ""} {
		o := NewOf[json.Number, int]()
		o.Set(n, 1)
		assertStdKey("json.Number "+string(n), o, map[json.Number]int{n: 1}, `{"`+string(n)+`":1}`)
	}
	ptr := NewOf[*keyPtrTxt, int]()
	ptr.Set(nil, 1)
	assertStdKey("nil TextMarshaler pointer", ptr, map[*keyPtrTxt]int{nil: 1}, `{"":1}`)
	ptr2 := NewOf[*keyPtrTxt, int]()
	ptr2.Set(&keyPtrTxt{S: "x"}, 1)
	assertStdKey("TextMarshaler pointer", ptr2, map[*keyPtrTxt]int{{S: "x"}: 1}, `{"Px":1}`)

	// 字符串键输出与旧版逐字节一致, escapeHTML 设置照常生效.
	names := NewOf[keyName, int]()
	names.Set("<a>", 1)
	if b, err := names.MarshalJSON(); err != nil || string(b) != "{\"\\u003ca\\u003e\"\n:1\n}" {
		t.Errorf("escaped string keys = %q, %v", b, err)
	}
	names.SetEscapeHTML(false)
	if b, err := names.MarshalJSON(); err != nil || string(b) != "{\"<a>\"\n:1\n}" {
		t.Errorf("unescaped string keys = %q, %v", b, err)
	}

	// encoding/json 同样不支持的键类型: 返回明确错误, 不再输出非法对象.
	for name, o := range map[string]json.Marshaler{
		"bool":   func() json.Marshaler { o := NewOf[bool, int](); o.Set(true, 1); return o }(),
		"float":  func() json.Marshaler { o := NewOf[float64, int](); o.Set(1.5, 1); return o }(),
		"struct": func() json.Marshaler { o := NewOf[keyStruct, int](); o.Set(keyStruct{1}, 1); return o }(),
		"nil":    func() json.Marshaler { o := NewOf[any, int](); o.Set(nil, 1); return o }(),
	} {
		if b, err := o.MarshalJSON(); err == nil {
			t.Errorf("%s keys: MarshalJSON = %q, want error", name, b)
		} else if !strings.Contains(err.Error(), "key") {
			t.Errorf("%s keys: unclear error %v", name, err)
		}
	}
}

// legacyMarshalOf 复刻 v1.4.2 的 MarshalJSON (键直接 encoder.Encode), 作为字符串键热路径的输出与分配基线.
func legacyMarshalOf[K comparable, V any](o *OrderedMapOf[K, V]) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(o.escapeHTML)
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		_ = encoder.Encode(k)
		buf.WriteByte(':')
		_ = encoder.Encode(o.values[k])
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// TestOrderedMapOf_StringKeyHotPathUnchanged 验证 string 键 (下游唯一用法) 的输出与分配次数与 v1.4.2 一致,
// 键类型适配只作用于非字符串键.
func TestOrderedMapOf_StringKeyHotPathUnchanged(t *testing.T) {
	o := NewOf[string, int]()
	for i := 0; i < 100; i++ {
		o.Set(fmt.Sprintf("key-<%d>", i), i)
	}
	got, err := o.MarshalJSON()
	if err != nil || string(got) != string(legacyMarshalOf(o)) {
		t.Fatalf("string key output differs from v1.4.2: %s, %v", got, err)
	}
	newAllocs := testing.AllocsPerRun(100, func() { _, _ = o.MarshalJSON() })
	oldAllocs := testing.AllocsPerRun(100, func() { _ = legacyMarshalOf(o) })
	if newAllocs > oldAllocs {
		t.Fatalf("string key MarshalJSON allocs = %v, v1.4.2 = %v", newAllocs, oldAllocs)
	}
}
