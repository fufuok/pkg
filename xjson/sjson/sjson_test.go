package sjson

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/xjson/gjson"
	"github.com/fufuok/pkg/xjson/pretty"
)

const (
	setRaw    = 1
	setBool   = 2
	setInt    = 3
	setFloat  = 4
	setString = 5
	setDelete = 6
)

func sortJSON(json string) string {
	opts := pretty.Options{SortKeys: true}
	return string(pretty.Ugly(pretty.PrettyOptions([]byte(json), &opts)))
}

func testRaw(t *testing.T, kind int, expect, json, path string, value any) {
	t.Helper()
	expect = sortJSON(expect)
	var json2 string
	var err error
	switch kind {
	default:
		json2, err = Set(json, path, value)
	case setRaw:
		json2, err = SetRaw(json, path, value.(string))
	case setDelete:
		json2, err = Delete(json, path)
	}

	if err != nil {
		t.Fatal(err)
	}
	json2 = sortJSON(json2)
	if json2 != expect {
		t.Fatalf("expected '%v', got '%v'", expect, json2)
	}
	var json3 []byte
	switch kind {
	default:
		json3, err = SetBytes([]byte(json), path, value)
	case setRaw:
		json3, err = SetRawBytes([]byte(json), path, []byte(value.(string)))
	case setDelete:
		json3, err = DeleteBytes([]byte(json), path)
	}
	json3 = []byte(sortJSON(string(json3)))
	if err != nil {
		t.Fatal(err)
	} else if string(json3) != expect {
		t.Fatalf("expected '%v', got '%v'", expect, string(json3))
	}
}

func TestBasic(t *testing.T) {
	testRaw(t, setRaw, `[{"hiw":"planet","hi":"world"}]`, `[{"hi":"world"}]`, "0.hiw", `"planet"`)
	testRaw(t, setRaw, `[true]`, ``, "0", `true`)
	testRaw(t, setRaw, `[null,true]`, ``, "1", `true`)
	testRaw(t, setRaw, `[1,null,true]`, `[1]`, "2", `true`)
	testRaw(t, setRaw, `[1,true,false]`, `[1,null,false]`, "1", `true`)
	testRaw(t, setRaw,
		`[1,{"hello":"when","this":[0,null,2]},false]`,
		`[1,{"hello":"when","this":[0,1,2]},false]`,
		"1.this.1", `null`)
	testRaw(t, setRaw,
		`{"a":1,"b":{"hello":"when","this":[0,null,2]},"c":false}`,
		`{"a":1,"b":{"hello":"when","this":[0,1,2]},"c":false}`,
		"b.this.1", `null`)
	testRaw(t, setRaw,
		`{"a":1,"b":{"hello":"when","this":[0,null,2,null,4]},"c":false}`,
		`{"a":1,"b":{"hello":"when","this":[0,null,2]},"c":false}`,
		"b.this.4", `4`)
	testRaw(t, setRaw,
		`{"b":{"this":[null,null,null,null,4]}}`,
		``,
		"b.this.4", `4`)
	testRaw(t, setRaw,
		`[null,{"this":[null,null,null,null,4]}]`,
		``,
		"1.this.4", `4`)
	testRaw(t, setRaw,
		`{"1":{"this":[null,null,null,null,4]}}`,
		``,
		":1.this.4", `4`)
	testRaw(t, setRaw,
		`{":1":{"this":[null,null,null,null,4]}}`,
		``,
		"\\:1.this.4", `4`)
	testRaw(t, setRaw,
		`{":\\1":{"this":[null,null,null,null,{".HI":4}]}}`,
		``,
		"\\:\\\\1.this.4.\\.HI", `4`)
	testRaw(t, setRaw,
		`{"app.token":"cde"}`,
		`{"app.token":"abc"}`,
		"app\\.token", `"cde"`)
	testRaw(t, setRaw,
		`{"b":{"this":{"😇":""}}}`,
		``,
		"b.this.😇", `""`)
	testRaw(t, setRaw,
		`[ 1,2  ,3]`,
		`  [ 1,2  ] `,
		"-1", `3`)
	testRaw(t, setInt, `[1234]`, ``, `0`, int64(1234))
	testRaw(t, setFloat, `[1234.5]`, ``, `0`, float64(1234.5))
	testRaw(t, setString, `["1234.5"]`, ``, `0`, "1234.5")
	testRaw(t, setBool, `[true]`, ``, `0`, true)
	testRaw(t, setBool, `[null]`, ``, `0`, nil)
	testRaw(t, setString, `{"arr":[1]}`, ``, `arr.-1`, 1)
	testRaw(t, setString, `{"a":"\\"}`, ``, `a`, "\\")
	testRaw(t, setString, `{"a":"C:\\Windows\\System32"}`, ``, `a`, `C:\Windows\System32`)
}

func TestDelete(t *testing.T) {
	testRaw(t, setDelete, `[456]`, `[123,456]`, `0`, nil)
	testRaw(t, setDelete, `[123,789]`, `[123,456,789]`, `1`, nil)
	testRaw(t, setDelete, `[123,456]`, `[123,456,789]`, `-1`, nil)
	testRaw(t, setDelete, `{"a":[123,456]}`, `{"a":[123,456,789]}`, `a.-1`, nil)
	testRaw(t, setDelete, `{"and":"another"}`, `{"this":"that","and":"another"}`, `this`, nil)
	testRaw(t, setDelete, `{"this":"that"}`, `{"this":"that","and":"another"}`, `and`, nil)
	testRaw(t, setDelete, `{}`, `{"and":"another"}`, `and`, nil)
	testRaw(t, setDelete, `{"1":"2"}`, `{"1":"2"}`, `3`, nil)
}

// TestRandomData is a fuzzing test that throws random data at SetRaw
// function looking for panics.
func TestRandomData(t *testing.T) {
	var lstr string
	defer func() {
		if v := recover(); v != nil {
			println("'" + hex.EncodeToString([]byte(lstr)) + "'")
			println("'" + lstr + "'")
			panic(v)
		}
	}()
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, 200)
	for range 2000000 {
		n, err := rand.Read(b[:rand.Int()%len(b)])
		if err != nil {
			t.Fatal(err)
		}
		lstr = string(b[:n])
		SetRaw(lstr, "zzzz.zzzz.zzzz", "123")
	}
}

func TestDeleteIssue21(t *testing.T) {
	json := `{"country_code_from":"NZ","country_code_to":"SA","date_created":"2018-09-13T02:56:11.25783Z","date_updated":"2018-09-14T03:15:16.67356Z","disabled":false,"last_edited_by":"Developers","id":"a3e...bc454","merchant_id":"f2b...b91abf","signed_date":"2018-02-01T00:00:00Z","start_date":"2018-03-01T00:00:00Z","url":"https://www.google.com"}`
	res1 := gjson.Get(json, "date_updated")
	var err error
	json, err = Delete(json, "date_updated")
	if err != nil {
		t.Fatal(err)
	}
	res2 := gjson.Get(json, "date_updated")
	res3 := gjson.Get(json, "date_created")
	if !res1.Exists() || res2.Exists() || !res3.Exists() {
		t.Fatal("bad news")
	}

	// We change the number of characters in this to make the section of the string before the section that we want to delete a certain length

	// ---------------------------
	lenBeforeToDeleteIs307AsBytes := `{"1":"","0":"012345678901234567890123456789012345678901234567890123456789012345678901234567","to_delete":"0","2":""}`

	expectedForLenBefore307AsBytes := `{"1":"","0":"012345678901234567890123456789012345678901234567890123456789012345678901234567","2":""}`
	// ---------------------------

	// ---------------------------
	lenBeforeToDeleteIs308AsBytes := `{"1":"","0":"0123456789012345678901234567890123456789012345678901234567890123456789012345678","to_delete":"0","2":""}`

	expectedForLenBefore308AsBytes := `{"1":"","0":"0123456789012345678901234567890123456789012345678901234567890123456789012345678","2":""}`
	// ---------------------------

	// ---------------------------
	lenBeforeToDeleteIs309AsBytes := `{"1":"","0":"01234567890123456789012345678901234567890123456789012345678901234567890123456","to_delete":"0","2":""}`

	expectedForLenBefore309AsBytes := `{"1":"","0":"01234567890123456789012345678901234567890123456789012345678901234567890123456","2":""}`
	// ---------------------------

	data := []struct {
		desc     string
		input    string
		expected string
	}{
		{
			desc:     "len before \"to_delete\"... = 307",
			input:    lenBeforeToDeleteIs307AsBytes,
			expected: expectedForLenBefore307AsBytes,
		},
		{
			desc:     "len before \"to_delete\"... = 308",
			input:    lenBeforeToDeleteIs308AsBytes,
			expected: expectedForLenBefore308AsBytes,
		},
		{
			desc:     "len before \"to_delete\"... = 309",
			input:    lenBeforeToDeleteIs309AsBytes,
			expected: expectedForLenBefore309AsBytes,
		},
	}

	for i, d := range data {
		result, err := Delete(d.input, "to_delete")
		if err != nil {
			t.Error(fmtErrorf(testError{
				unexpected: "error",
				desc:       d.desc,
				i:          i,
				lenInput:   len(d.input),
				input:      d.input,
				expected:   d.expected,
				result:     result,
			}))
		}
		if result != d.expected {
			t.Error(fmtErrorf(testError{
				unexpected: "result",
				desc:       d.desc,
				i:          i,
				lenInput:   len(d.input),
				input:      d.input,
				expected:   d.expected,
				result:     result,
			}))
		}
	}
}

type testError struct {
	unexpected string
	desc       string
	i          int
	lenInput   int
	input      any
	expected   any
	result     any
}

func fmtErrorf(e testError) string {
	return fmt.Sprintf(
		"Unexpected %s:\n\t"+
			"for=%q\n\t"+
			"i=%d\n\t"+
			"len(input)=%d\n\t"+
			"input=%v\n\t"+
			"expected=%v\n\t"+
			"result=%v",
		e.unexpected, e.desc, e.i, e.lenInput, e.input, e.expected, e.result,
	)
}

func TestSetDotKeyIssue10(t *testing.T) {
	json := `{"app.token":"abc"}`
	json, _ = Set(json, `app\.token`, "cde")
	if json != `{"app.token":"cde"}` {
		t.Fatalf("expected '%v', got '%v'", `{"app.token":"cde"}`, json)
	}
}

func TestDeleteDotKeyIssue19(t *testing.T) {
	json := []byte(`{"data":{"key1":"value1","key2.something":"value2"}}`)
	json, _ = DeleteBytes(json, `data.key2\.something`)
	if string(json) != `{"data":{"key1":"value1"}}` {
		t.Fatalf("expected '%v', got '%v'", `{"data":{"key1":"value1"}}`, json)
	}
}

func TestIssue36(t *testing.T) {
	json := `
	{
	    "size": 1000
    }
`
	raw := `
	{
	    "sample": "hello"
	}
`
	_ = raw
	if true {
		json, _ = SetRaw(json, "aggs", raw)
	}
	if !gjson.Valid(json) {
		t.Fatal("invalid json")
	}
	res := gjson.Get(json, "aggs.sample").String()
	if res != "hello" {
		t.Fatal("unexpected result")
	}
}

var example = `
{
	"name": {"first": "Tom", "last": "Anderson"},
	"age":37,
	"children": ["Sara","Alex","Jack"],
	"fav.movie": "Deer Hunter",
	"friends": [
	  {"first": "Dale", "last": "Murphy", "age": 44, "nets": ["ig", "fb", "tw"]},
	  {"first": "Roger", "last": "Craig", "age": 68, "nets": ["fb", "tw"]},
	  {"first": "Jane", "last": "Murphy", "age": 47, "nets": ["ig", "tw"]}
	]
  }
  `

func TestIndex(t *testing.T) {
	path := `friends.#(last="Murphy").last`
	json, err := Set(example, path, "Johnson")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.Get(json, "friends.#.last").String() != `["Johnson","Craig","Murphy"]` {
		t.Fatal("mismatch")
	}
}

func TestIndexes(t *testing.T) {
	path := `friends.#(last="Murphy")#.last`
	json, err := Set(example, path, "Johnson")
	if err != nil {
		t.Fatal(err)
	}
	if gjson.Get(json, "friends.#.last").String() != `["Johnson","Craig","Johnson"]` {
		t.Fatal("mismatch")
	}
}

func TestIssue61(t *testing.T) {
	json := `{
		"@context": {
		  "rdfs": "http://www.w3.org/2000/01/rdf-schema#",
		  "@vocab": "http://schema.org/",
		  "sh": "http://www.w3.org/ns/shacl#"
		}
	}`
	json1, _ := Set(json, "@context.@vocab", "newval")
	if gjson.Get(json1, "@context.@vocab").String() != "newval" {
		t.Fail()
	}
}

// TestDeleteOptions 验证新增删除入口在 nil 和优化提示下保留路径, 错误及字符串只读语义.
func TestDeleteOptions(t *testing.T) {
	for _, test := range []struct {
		name, input, path, want string
		wantErr                 bool
	}{
		{"object", `{"a":1,"b":2}`, "a", `{"b":2}`, false},
		{"array", `[1,2,3]`, "1", `[1,3]`, false},
		{"escaped_key", `{"a.b":1,"c":2}`, `a\.b`, `{"c":2}`, false},
		{"missing", `{"a":1}`, "b", `{"a":1}`, false},
		// 上游 v1.2.7 起空路径表示空字符串键, 不存在时视为无变化.
		{"empty_path", `{"a":1}`, "", `{"a":1}`, false},
		{"empty_key", `{"":1,"a":2}`, "", `{"a":2}`, false},
		{"array_last", `[1,2,3]`, "-1", `[1,2]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, options := range []*Options{nil, {}, {Optimistic: true, ReplaceInPlace: true}} {
				var saved Options
				if options != nil {
					saved = *options
				}
				input := test.input
				got, err := DeleteOptions(input, test.path, options)
				if got != test.want || (err != nil) != test.wantErr {
					t.Errorf("DeleteOptions = (%q, %v), want (%q, error=%t)", got, err, test.want, test.wantErr)
				}
				if input != test.input || (options != nil && *options != saved) {
					t.Fatal("DeleteOptions modified input or options")
				}
				gotBytes, err := DeleteBytesOptions([]byte(test.input), test.path, options)
				if string(gotBytes) != test.want || (err != nil) != test.wantErr {
					t.Errorf("DeleteBytesOptions = (%q, %v), want (%q, error=%t)", gotBytes, err, test.want, test.wantErr)
				}
			}
		})
	}
}

func TestIssue97(t *testing.T) {
	json, err := Set(`{"rate":0}`, "rate", math.NaN())
	if err == nil {
		t.FailNow()
	}
	if json != `{"rate":"NaN"}` {
		t.FailNow()
	}
	json, err = Set(`{"rate":0}`, "rate", math.Inf(-1))
	if err == nil {
		t.FailNow()
	}
	if json != `{"rate":"-Inf"}` {
		t.FailNow()
	}
	json, err = Set(`{"rate":0}`, "rate", math.Inf(0))
	if err == nil {
		t.FailNow()
	}
	if json != `{"rate":"+Inf"}` {
		t.FailNow()
	}
}

func TestIssue77(t *testing.T) {
	value, err := Delete(`{"":"1","1":"2"}`, "")
	if value != `{"1":"2"}` || err != nil {
		t.FailNow()
	}
}

func TestIssue98(t *testing.T) {
	// 2^64 wraps to 0 -> [4,2,3]? appends at a wrapped index, returns [1,2,3,4]
	json, err := Set(`[1,2,3]`, "18446744073709551616", 4)
	if json != "[1,2,3]" || err == nil {
		t.FailNow()
	}
	if err.Error() != "cannot set array element for non-numeric key '18446744073709551616'" {
		t.FailNow()
	}
	json, err = Set(`[1,2,3]`, "100001", 4)
	if err != nil {
		t.FailNow()
	}
	json, err = Set(`[1,2,3]`, "100003", 4)
	if err != nil {
		t.FailNow()
	}
	json, err = Set(`[1,2,3]`, "100004", 4)
	if json != "[1,2,3]" || err == nil {
		t.FailNow()
	}
	if err.Error() != "cannot pad array for key '100004', has 3 elements, limit is 100000" {
		t.FailNow()
	}

	json, err = Set("", "b.this.4", "5")
	if json != `{"b":{"this":[null,null,null,null,"5"]}}` || err != nil {
		t.FailNow()
	}
	json, err = Set("", "b.this.100001", "5")
	if json != "" || err == nil {
		t.FailNow()
	}
	if err.Error() != "cannot pad array for key '100001', has 0 elements, limit is 100000" {
		t.FailNow()
	}
	json, err = Set("", "b.this.18446744073709551616", "5")
	if json != `{"b":{"this":{"18446744073709551616":"5"}}}` {
		t.FailNow()
	}

	// testRaw(t, setRaw,
	// 	`{"b":{"this":[null,null,null,null,4]}}`,
	// 	``,
	// 	"b.this.4", `4`)
}

func TestIssue99(t *testing.T) {
	json, err := Delete(`{"#":200,"-1":true}`, "-1")
	if json != `{"#":200}` || err != nil {
		t.FailNow()
	}
}

// TestUpstreamBoundaries 补充上游 v1.2.6~v1.2.9 行为的本地边界: 非有限浮点, 补齐上限与空键.
func TestUpstreamBoundaries(t *testing.T) {
	// float32 非有限值与 float64 走同一路径, 结果保持合法 JSON 且返回错误.
	got, err := SetBytes([]byte(`{"v":0}`), "v", float32(math.Inf(1)))
	if string(got) != `{"v":"+Inf"}` || err == nil {
		t.Fatalf("float32 Inf = (%s, %v)", got, err)
	}
	if !gjson.ValidBytes(got) {
		t.Fatalf("result must stay valid JSON: %s", got)
	}
	// 有限浮点不受影响.
	if got, err := Set(`{"v":0}`, "v", 1.5); got != `{"v":1.5}` || err != nil {
		t.Fatalf("finite float = (%s, %v)", got, err)
	}

	// 恰好补齐 maxpad 个 null 允许, 多一个即拒绝且不修改输入.
	if _, err := Set(`[]`, strconv.Itoa(maxpad), 1); err != nil {
		t.Fatalf("pad exactly maxpad: %v", err)
	}
	if got, err := Set(`[]`, strconv.Itoa(maxpad+1), 1); got != `[]` || err == nil {
		t.Fatalf("pad over maxpad = (%s, %v)", got, err)
	}
	// 超出 int 范围的片段按对象键处理, 不再回绕成小索引.
	if got, err := Set(``, "a.99999999999999999999", 1); got != `{"a":{"99999999999999999999":1}}` || err != nil {
		t.Fatalf("overflow key = (%s, %v)", got, err)
	}

	// 空路径写入空字符串键.
	if got, err := Set(`{"a":1}`, "", 2); got != `{"a":1,"":2}` || err != nil {
		t.Fatalf("set empty key = (%s, %v)", got, err)
	}
}

// TestComplexPathUnlocatable 覆盖本地修正: # 后接计算值时 gjson 偏移不指向原文, 不得 panic 或拼出非法 JSON.
func TestComplexPathUnlocatable(t *testing.T) {
	for _, c := range [][2]string{
		{`[1]`, "#.!true"},
		{`{"a":[{"b":1}]}`, "a.#.@pretty"},
		{`{"a":[[1,2],[3,4]]}`, "a.#.#"},
		{`[[null,[]]]`, "#.@reverse"},
	} {
		got, err := Set(c[0], c[1], "Z")
		if err != nil || got != c[0] {
			t.Fatalf("Set(%s, %q) = (%s, %v), want unchanged", c[0], c[1], got, err)
		}
	}
	// DataRouter query2JSON 形态: 外部查询键逐个作为路径写入同一正文.
	var body []byte
	for _, k := range []string{"0", "#.!true", "#.@this"} {
		body, _ = SetBytes(body, k, "1")
	}
	if string(body) != `["1"]` {
		t.Fatalf("query keys = %s", body)
	}
	// 可定位的复杂路径保持原有行为.
	for _, c := range [][3]string{
		{`{"a":[{"b":1},{"b":2}]}`, "a.#.b", `{"a":[{"b":9},{"b":9}]}`},
		{`{"a":[{"b":1},{"b":2}]}`, "a.#(b==2).b", `{"a":[{"b":1},{"b":9}]}`},
		{`{"a":[{"b":1},{"b":2}]}`, "a.#(b>0)#.b", `{"a":[{"b":9},{"b":9}]}`},
		// JSON Lines 查询命中首行时偏移为 0, 仍应写入 (与上游一致).
		{"{\"a\":1}\n{\"a\":2}", "..#(a==1)#", "9\n{\"a\":2}"},
		{"1\n2", "..#(==1)#", "9\n2"},
		{"{\"a\":1}\n{\"a\":2}", "..#.a", "{\"a\":9}\n{\"a\":9}"},
	} {
		if got, err := Set(c[0], c[1], 9); got != c[2] || err != nil {
			t.Fatalf("Set(%s, %q) = (%s, %v), want %s", c[0], c[1], got, err, c[2])
		}
	}
}

// TestDeleteKeyLeadingEscapedQuote 覆盖本地修正: 键以转义引号开头时只删除该键.
func TestDeleteKeyLeadingEscapedQuote(t *testing.T) {
	q := string(rune(92)) + `"`
	for _, c := range [][2]string{
		{`{"x":1,"` + q + `":2}`, `{"x":1}`},
		{`{"` + q + `":1,"y":2}`, `{"y":2}`},
		{`{"a":1,"` + q + `a":2,"b":3}`, `{"a":1,"b":3}`},
	} {
		path := q
		if strings.Contains(c[0], q+"a") {
			path = q + "a"
		}
		if got, err := Delete(c[0], path); got != c[1] || err != nil {
			t.Fatalf("Delete(%s, %s) = (%s, %v), want %s", c[0], path, got, err, c[1])
		}
	}
}

// TestOptimisticInPlaceEscape 覆盖本地修正: 原地替换遇到需要转义的字符串时仍须写入.
func TestOptimisticInPlaceEscape(t *testing.T) {
	opts := &Options{Optimistic: true, ReplaceInPlace: true}
	for _, v := range []string{"é", `a"b`, "x\ny", "中文"} {
		in := []byte(`{"a":"xxxxxxxxxxxxxxxx","b":1}`)
		got, err := SetBytesOptions(in, "a", v, opts)
		if err != nil || gjson.GetBytes(got, "a").String() != v || gjson.GetBytes(got, "b").Int() != 1 {
			t.Fatalf("SetBytesOptions(%q) = (%s, %v)", v, got, err)
		}
	}
	// 无需转义时仍走原地路径.
	in := []byte(`{"a":"xxxxxxxxxxxxxxxx","b":1}`)
	got, err := SetBytesOptions(in, "a", "ok", opts)
	if err != nil || string(got) != `{"a":"ok","b":1}` || &got[0] != &in[0] {
		t.Fatalf("in-place ascii = (%s, %v)", got, err)
	}
}

// TestPadTotalBudget 覆盖本地修正: maxpad 限制单次调用的累计补齐, 多层路径不能逐层放大.
func TestPadTotalBudget(t *testing.T) {
	// 701 字节路径逐层补齐 100000 个 null: 上游生成约 50MB, 本地拒绝且不修改输入.
	path := "a" + strings.Repeat(".100000", 100)
	got, err := Set(`{"x":1}`, path, 1)
	if got != `{"x":1}` || err == nil || !strings.Contains(err.Error(), "total padding") {
		t.Fatalf("multi-level pad = (%d bytes, %v)", len(got), err)
	}
	if b, err := SetBytes(nil, path, 1); err == nil || len(b) != 0 {
		t.Fatalf("multi-level pad on empty = (%d bytes, %v)", len(b), err)
	}

	// 新建多层数组: 累计恰好 maxpad 允许, 多一个即拒绝.
	half := strconv.Itoa(maxpad / 2)
	if _, err := Set(``, "a."+half+"."+half, 1); err != nil {
		t.Fatalf("total exactly maxpad: %v", err)
	}
	if _, err := Set(``, "a."+half+"."+strconv.Itoa(maxpad/2+1), 1); err == nil {
		t.Fatal("total over maxpad must fail")
	}

	// 扩容已有数组后再新建下层数组: 两段补齐合计计入同一预算.
	// [1,2,3] 写下标 60003 需补 60000 个 null, 下层再补 40000 个, 合计恰好 maxpad.
	if _, err := Set(`[1,2,3]`, "60003.40000", 1); err != nil {
		t.Fatalf("grow plus build within budget: %v", err)
	}
	got, err = Set(`[1,2,3]`, "60003.40001", 1)
	if got != `[1,2,3]` || err == nil {
		t.Fatalf("grow plus build over budget = (%d bytes, %v)", len(got), err)
	}

	// 单层超限仍返回上游原有错误信息.
	if _, err := Set(``, "a."+strconv.Itoa(maxpad+1), 1); err == nil || strings.Contains(err.Error(), "total padding") {
		t.Fatalf("single level error = %v", err)
	}

	// 上层已补齐后, 下层单层超限也按单层报错; 下标为 math.MaxInt 时累计值不得溢出成负数.
	maxInt := strconv.Itoa(math.MaxInt)
	want := "cannot pad array for key '" + maxInt + "', has 0 elements, limit is 100000"
	for _, c := range []struct{ json, path string }{
		{``, "a.1." + maxInt},
		{``, "a.1." + strconv.Itoa(maxpad+1)},
		{`{"a":[1,2,3]}`, "a.5." + maxInt},
	} {
		got, err := Set(c.json, c.path, 1)
		if got != c.json || err == nil {
			t.Fatalf("Set(%q, %q) = (%q, %v)", c.json, c.path, got, err)
		}
		if strings.Contains(err.Error(), "total padding") || strings.Contains(err.Error(), "-") {
			t.Fatalf("Set(%q, %q) error = %v", c.json, c.path, err)
		}
		if strings.HasSuffix(c.path, maxInt) && err.Error() != want {
			t.Fatalf("Set(%q, %q) error = %q, want %q", c.json, c.path, err, want)
		}
	}
}

// TestComplexPathMisplaced 覆盖本地修正: gjson 偏移不指向原文而内容碰巧相同时, 不得写到别处.
func TestComplexPathMisplaced(t *testing.T) {
	// 均应原样返回且无错误: 管道后的 Indexes 相对子值, # 后接静态值时偏移指向父元素,
	// 数字前缀 ("1" 落在 "11111" 上), 以及静态值内容等于整个文档 (偏移 0).
	for _, c := range [][2]string{
		{`{"xy":1,"a":[{"b":1}]}`, "a|#.b"},
		{`{"x":11111,"a":[{"b":1}]}`, "a|#.b"},
		{`[12,13]`, "#(>10)#.!1"},
		{`[1]`, "#.![1]"},
		{`[{"a":1}]`, "#.{a}"},
		{`[[1]]`, "#.[0]"},
		{`[1,2]`, "#.@this"},
		{`{"a":[{"b":1}]}`, `a.#(b=="|").b`},
		// 无匹配时 gjson 返回计算出的 "[]", 偏移 0 且内容恰好等于整个文档.
		{`[]`, "..#(a==1)#"},
		{`[]`, "#(a==1)#"},
		{"[]\n[]", "..#(a==1)#"},
		// 嵌套多结果只记录外层元素偏移, 会把整个外层元素替换掉.
		{`{"m":[[],[]]}`, "m.#.#(x==1)#"},
		{`[[1,2]]`, "#(#>0)#.#(>0)#"},
		{`[1,2]`, "..#.#(>0)#"},
		{`{"a":[{"b":[{"c":1}]}]}`, "a.#.b.#.c"},
		// 查询体与 gjson 解析不一致的构造路径: 转义的 "#(", 段中的 "#(", "]" 闭合, 对象上的 "#(" 不是查询.
		{`{"xy":1,"#(":[{"b":1}]}`, `\#(|#.b`},
		{`{"xy":1,"a#(":[{"b":1}]}`, "a#(|#.b"},
		{`{"xy":1,"a":[[{"b":1}]]}`, "a.#(#>0]#|0|#.b"},
		{`{"xyzwv":1,"a":{"#(b==\"":[{"c\")":1}]}}`, `a.#(b=="|#.c")`},
		// 查询值含 '|' 时无法区分查询与管道, 保守拒绝.
		{`{"a":[{"b":"x|y"},{"b":"z"}]}`, `a.#(b=="x|y")#.b`},
	} {
		if got, err := Set(c[0], c[1], 9); got != c[0] || err != nil {
			t.Fatalf("Set(%s, %q) = (%s, %v), want unchanged", c[0], c[1], got, err)
		}
	}
	// 按 DataRouter query2JSON 的方式逐个写入外部查询键, 后一个键不得改写前一个键的值.
	var body []byte
	for _, kv := range [][2]string{{"xy", "1"}, {"a.0.b", "1"}, {"a|#.b", "hacked"}, {"0", "1"}, {`#.!["1"]`, "x"}} {
		body, _ = SetBytes(body, kv[0], kv[1])
	}
	if string(body) != `{"xy":"1","a":[{"b":"1"}],"0":"1"}` {
		t.Fatalf("query2JSON style = %s", body)
	}
	// 偏移可信的路径照常写入; 段中间的 '@'/'!' 及未注册修饰符名是普通键名.
	for _, c := range [][3]string{
		{`{"a":[{"b":1},{"b":2}]}`, "a.#.b", `{"a":[{"b":9},{"b":9}]}`},
		{`{"a":[{"b":1},{"b":2}]}`, "a.#(b==2).b", `{"a":[{"b":1},{"b":9}]}`},
		{`{"a":[{"b":1},{"b":2}]}`, `a.#(b>1)#.b`, `{"a":[{"b":1},{"b":9}]}`},
		{`{"a":[{"b":"x"},{"b":"z"}]}`, `a.#(b=="x")#.b`, `{"a":[{"b":9},{"b":"z"}]}`},
		{`{"a":[{"n":["fb"],"f":1},{"n":["x"],"f":2}]}`, `a.#(n.#(=="fb"))#.f`, `{"a":[{"n":["fb"],"f":9},{"n":["x"],"f":2}]}`},
		{`{"a":[{"@ts":1},{"@ts":2}]}`, "a.#.@ts", `{"a":[{"@ts":9},{"@ts":9}]}`},
		{`[{"!x":1},{"!x":2}]`, "#.!x", `[{"!x":9},{"!x":9}]`},
		// 单匹配查询后再取多结果: gjson 已补上匹配元素的偏移 (上游写到第 1 个元素或字符串内部).
		{`{"a":[1,1,1,1,1,1,1,{"b":1,"c":[1]}]}`, "a.#(b==1).c.#(==1)#", `{"a":[1,1,1,1,1,1,1,{"b":1,"c":[9]}]}`},
		{`{"x":"aaaaa,1,b","a":[{"b":1,"c":[1]}]}`, "a.#(b==1).c.#(>0)#", `{"x":"aaaaa,1,b","a":[{"b":1,"c":[9]}]}`},
		{`{"a":[{"b":1,"c":[{"d":1}]},{"b":2,"c":[{"d":1}]}]}`, "a.#(b==2).c.#.d", `{"a":[{"b":1,"c":[{"d":1}]},{"b":2,"c":[{"d":9}]}]}`},
		{
			"{\"id\":1,\"t\":[{\"v\":\"x\"}]}\n{\"id\":3,\"t\":[{\"v\":\"x\"}]}", "..#(id==3).t.#.v",
			"{\"id\":1,\"t\":[{\"v\":\"x\"}]}\n{\"id\":3,\"t\":[{\"v\":9}]}",
		},
		{`{"@timestamp":1}`, "@timestamp", `{"@timestamp":9}`},
		{`{"a":{"b@c!":1}}`, "a.b@c!", `{"a":{"b@c!":9}}`},
		// JSON Lines: 首行偏移为 0 也可写入, 单结果与多结果一致.
		{"{\"a\":1}\n{\"a\":2}", "..#(a==1)", "9\n{\"a\":2}"},
		{"{\"a\":1}\n{\"a\":2}", "..#(a==2)", "{\"a\":1}\n9"},
		{"{\"a\":1}\n{\"a\":2}", "..#.a", "{\"a\":9}\n{\"a\":9}"},
	} {
		if got, err := Set(c[0], c[1], 9); got != c[2] || err != nil {
			t.Fatalf("Set(%s, %q) = (%s, %v), want %s", c[0], c[1], got, err, c[2])
		}
	}
}

// TestAtouiNoAlloc 覆盖本地修正: 超过 8 字节的非数字键名不调用 ParseUint, 不分配.
func TestAtouiNoAlloc(t *testing.T) {
	r := pathResult{part: "q_utm_source"}
	if n := testing.AllocsPerRun(100, func() { _, _ = atoui(r) }); n != 0 {
		t.Fatalf("atoui allocs = %v", n)
	}
	for _, c := range []struct {
		part string
		n    int
		ok   bool
	}{
		{"", 0, true},
		{"0", 0, true},
		{"123456789", 123456789, true},
		{"12345678a", 0, false},
		{strconv.Itoa(math.MaxInt), math.MaxInt, true},
		{strconv.FormatUint(uint64(math.MaxInt)+1, 10), 0, false},
		{"18446744073709551616", 0, false},
		{"-1", 0, false},
	} {
		n, ok := atoui(pathResult{part: c.part})
		if ok != c.ok || (ok && n != c.n) {
			t.Fatalf("atoui(%q) = (%d, %v), want (%d, %v)", c.part, n, ok, c.n, c.ok)
		}
	}
}
