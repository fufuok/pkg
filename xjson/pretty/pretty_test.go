package pretty

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func j(js any) string {
	var v any
	if err := json.Unmarshal(fmt.Appendf(nil, "%s", js), &v); err != nil {
		fmt.Printf(">>%s<<\n", js)
		panic(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

var example1 = []byte(`
{
	"name": {
		"last": "Sanders",
		"first": "Janet"
	}, 
	"children": [
		"Andy", "Carol", "Mike"
	],
	"values": [
		10.10, true, false, null, "hello", {}
	],
	"values2": {},
	"values3": [],
	"deep": {"deep":{"deep":[1,2,3,4,5]}}
}
`)

var example2 = `[ 0, 10, 10.10, true, false, null, "hello \" "]`

func assertEqual(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("Not equal\n\t'%v'\n\t'%v'", a, b)
	}
}

func TestPretty(t *testing.T) {
	pretty := Pretty(Ugly(Pretty([]byte(example1))))
	assertEqual(t, j(pretty), j(pretty))
	assertEqual(t, j(example1), j(pretty))
	pretty = Pretty(Ugly(Pretty([]byte(example2))))
	assertEqual(t, j(pretty), j(pretty))
	assertEqual(t, j(example2), j(pretty))
	pretty = Pretty([]byte(" "))
	assertEqual(t, "", string(pretty))
	opts := *DefaultOptions
	opts.SortKeys = true
	pretty = PrettyOptions(Ugly(Pretty([]byte(example2))), &opts)
	assertEqual(t, j(pretty), j(pretty))
	assertEqual(t, j(example2), j(pretty))
}

func TestUgly(t *testing.T) {
	ugly := Ugly([]byte(example1))
	var buf bytes.Buffer
	err := json.Compact(&buf, []byte(example1))
	assertEqual(t, nil, err)
	assertEqual(t, buf.Bytes(), ugly)
	ugly = UglyInPlace(ugly)
	assertEqual(t, nil, err)
	assertEqual(t, buf.Bytes(), ugly)
}

func TestRandom(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	for range 100000 {
		b := make([]byte, 1024)
		rand.Read(b)
		Pretty(b)
		Ugly(b)
	}
}

func TestBig(t *testing.T) {
	json := `[
  {
    "_id": "58d19e070f4898817162964a",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "65d46c3e-9d3a-4bfe-bab2-252f36a53c6b",
    "isActive": false,
    "balance": "$1,064.00",
    "picture": "http://placehold.it/32x32",
    "age": 37,
    "eyeColor": "brown",
    "name": "Chan Orr",
    "gender": "male",
    "company": "SURETECH",
    "email": "chanorr@suretech.com",
    "phone": "+1 (808) 496-3754",
    "address": "792 Bushwick Place, Glenbrook, Vermont, 9893",
    "about": "Amet consequat eu enim laboris cillum ad laboris in quis laboris reprehenderit. Eu deserunt occaecat dolore eu veniam non dolore et magna ex incididunt. Ea dolor laboris ex officia culpa laborum amet adipisicing laboris tempor magna elit mollit ad. Tempor ex aliqua mollit enim laboris sunt fugiat. Sint sunt ex est non dolore consectetur culpa ullamco id dolor nulla labore. Sunt duis fugiat cupidatat sunt deserunt qui aute elit consequat sint cupidatat. Consequat ullamco aliqua nulla velit tempor aute.\r\n",
    "registered": "2014-08-04T04:09:10 +07:00",
    "latitude": 80.707807,
    "longitude": 18.857548,
    "tags": [
      "consectetur",
      "est",
      "cupidatat",
      "nisi",
      "incididunt",
      "aliqua",
      "ullamco"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Little Edwards"
      },
      {
        "id": 1,
        "name": "Gay Johns"
      },
      {
        "id": 2,
        "name": "Hoover Noble"
      }
    ],
    "greeting": "Hello, Chan Orr! You have 3 unread messages.",
    "favoriteFruit": "banana"
  },
  {
    "_id": "58d19e07c2119248f8fa11ff",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "b362f0a0-d1ed-4b94-9d6b-213712620a20",
    "isActive": false,
    "balance": "$1,321.26",
    "picture": "http://placehold.it/32x32",
    "age": 28,
    "eyeColor": "blue",
    "name": "Molly Hyde",
    "gender": "female",
    "company": "QUALITEX",
    "email": "mollyhyde@qualitex.com",
    "phone": "+1 (849) 455-2934",
    "address": "440 Visitation Place, Bridgetown, Palau, 5053",
    "about": "Ipsum reprehenderit nulla est nostrud ad incididunt officia in commodo id esse id. Ullamco ullamco commodo mollit ut id cupidatat veniam nostrud minim duis qui sit. Occaecat esse nostrud velit qui non dolor proident. Ipsum ipsum anim non mollit minim voluptate amet irure in. Sunt commodo occaecat aute ullamco sunt fugiat laboris culpa Lorem anim. Aliquip tempor excepteur labore aute deserunt consectetur incididunt aute eu est ullamco consectetur excepteur. Sunt sint consequat cupidatat nisi exercitation minim enim occaecat esse ex amet ex non.\r\n",
    "registered": "2014-09-12T08:51:11 +07:00",
    "latitude": 15.867177,
    "longitude": 165.862595,
    "tags": [
      "enim",
      "sint",
      "elit",
      "laborum",
      "elit",
      "cupidatat",
      "ipsum"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Holmes Hurley"
      },
      {
        "id": 1,
        "name": "Rhoda Spencer"
      },
      {
        "id": 2,
        "name": "Tommie Gallegos"
      }
    ],
    "greeting": "Hello, Molly Hyde! You have 10 unread messages.",
    "favoriteFruit": "banana"
  },
  {
    "_id": "58d19e07fc27eedd9159d710",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "1d343fd3-44f7-4246-a5e6-a9297afb3146",
    "isActive": false,
    "balance": "$1,459.65",
    "picture": "http://placehold.it/32x32",
    "age": 26,
    "eyeColor": "brown",
    "name": "Jaime Kennedy",
    "gender": "female",
    "company": "RECRITUBE",
    "email": "jaimekennedy@recritube.com",
    "phone": "+1 (983) 483-3522",
    "address": "997 Vanderveer Street, Alamo, Marshall Islands, 4767",
    "about": "Qui consequat veniam ex enim excepteur aliqua dolor duis Lorem deserunt. Lorem occaecat laboris quis nisi Lorem aute exercitation consectetur officia velit aliqua aliquip commodo. Tempor irure ad ipsum aliquip. Incididunt mollit aute cillum non magna duis officia anim laboris deserunt voluptate.\r\n",
    "registered": "2015-08-31T06:51:25 +07:00",
    "latitude": -7.486839,
    "longitude": 57.659287,
    "tags": [
      "veniam",
      "aliqua",
      "aute",
      "amet",
      "laborum",
      "quis",
      "sint"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Brown Christensen"
      },
      {
        "id": 1,
        "name": "Robyn Whitehead"
      },
      {
        "id": 2,
        "name": "Dolly Weaver"
      }
    ],
    "greeting": "Hello, Jaime Kennedy! You have 3 unread messages.",
    "favoriteFruit": "banana"
  },
  {
    "_id": "58d19e0783c362da4b71240d",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "dbe60229-60d2-4879-82f3-d9aca0baaf6f",
    "isActive": false,
    "balance": "$3,221.63",
    "picture": "http://placehold.it/32x32",
    "age": 32,
    "eyeColor": "green",
    "name": "Cherie Vinson",
    "gender": "female",
    "company": "SLAX",
    "email": "cherievinson@slax.com",
    "phone": "+1 (905) 474-3132",
    "address": "563 Macdougal Street, Navarre, New York, 8733",
    "about": "Ad laborum et magna quis veniam duis magna consectetur mollit in minim non officia aliquip. Ullamco dolor qui consectetur adipisicing. Incididunt ad ad incididunt duis velit laboris. Reprehenderit ullamco magna quis exercitation excepteur nisi labore pariatur laborum consequat eu laboris amet velit. Et dolore aliqua proident sunt dolore incididunt dolore fugiat ipsum tempor occaecat.\r\n",
    "registered": "2015-03-19T08:48:47 +07:00",
    "latitude": -56.480034,
    "longitude": -59.894094,
    "tags": [
      "irure",
      "commodo",
      "quis",
      "cillum",
      "quis",
      "nulla",
      "irure"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Danielle Mullins"
      },
      {
        "id": 1,
        "name": "Maxine Peters"
      },
      {
        "id": 2,
        "name": "Francine James"
      }
    ],
    "greeting": "Hello, Cherie Vinson! You have 1 unread messages.",
    "favoriteFruit": "apple"
  },
  {
    "_id": "58d19e07b8f1ea8e3451870d",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "91fd9527-770c-4006-a0ed-64ca0d819199",
    "isActive": true,
    "balance": "$2,387.38",
    "picture": "http://placehold.it/32x32",
    "age": 37,
    "eyeColor": "blue",
    "name": "Glenna Hanson",
    "gender": "female",
    "company": "ACUMENTOR",
    "email": "glennahanson@acumentor.com",
    "phone": "+1 (965) 564-3926",
    "address": "323 Seigel Street, Rosedale, Florida, 2700",
    "about": "Commodo id ex velit nulla incididunt occaecat aliquip ullamco consequat est. Esse officia adipisicing magna et et incididunt sit deserunt ex mollit id. Laborum proident sit sit duis proident cillum irure aliquip et commodo.\r\n",
    "registered": "2014-06-29T02:48:04 +07:00",
    "latitude": -6.141759,
    "longitude": 155.991532,
    "tags": [
      "amet",
      "pariatur",
      "culpa",
      "eu",
      "commodo",
      "magna",
      "excepteur"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Blanchard Blackburn"
      },
      {
        "id": 1,
        "name": "Ayers Guy"
      },
      {
        "id": 2,
        "name": "Powers Salinas"
      }
    ],
    "greeting": "Hello, Glenna Hanson! You have 4 unread messages.",
    "favoriteFruit": "strawberry"
  },
  {
    "_id": "58d19e07f1ad063dac8b72dc",
    "index": "<ReferenceError: indexkk is not defined>",
    "guid": "9b8c6cef-cfcd-4e6d-85e4-fe2e6920ec31",
    "isActive": true,
    "balance": "$1,828.58",
    "picture": "http://placehold.it/32x32",
    "age": 29,
    "eyeColor": "green",
    "name": "Hays Shields",
    "gender": "male",
    "company": "ISOLOGICA",
    "email": "haysshields@isologica.com",
    "phone": "+1 (882) 469-3201",
    "address": "574 Columbus Place, Singer, Georgia, 8716",
    "about": "Consectetur et adipisicing ad quis incididunt qui labore et ex elit esse. Ad elit officia ullamco dolor reprehenderit. Sunt nisi ullamco mollit incididunt consectetur nostrud anim adipisicing ullamco aliqua eiusmod ad. Et excepteur voluptate adipisicing velit id quis duis Lorem id deserunt esse irure Lorem. Est irure sint Lorem aliqua adipisicing velit irure Lorem. Ex in culpa laborum nostrud esse eu laboris velit. Anim excepteur ex ipsum amet nostrud cillum.\r\n",
    "registered": "2014-02-10T07:17:14 +07:00",
    "latitude": -66.354543,
    "longitude": 138.400461,
    "tags": [
      "mollit",
      "labore",
      "id",
      "labore",
      "dolor",
      "in",
      "elit"
    ],
    "friends": [
      {
        "id": 0,
        "name": "Mendoza Craig"
      },
      {
        "id": 1,
        "name": "Rowena Carey"
      },
      {
        "id": 2,
        "name": "Barry Francis"
      }
    ],
    "greeting": "Hello, Hays Shields! You have 10 unread messages.",
    "favoriteFruit": "strawberry"
  }
]`

	opts := *DefaultOptions
	opts.SortKeys = true
	jsonb := PrettyOptions(Ugly([]byte(json)), &opts)
	assertEqual(t, j(jsonb), j(json))
}

func TestColor(t *testing.T) {
	json := string(Pretty([]byte(`
{"hello":"world","what":123,
"arr":["1","2",1,2,true,false,null],
"obj":{"key1":null,"ar` + "\x1B[36m" + `cyan` + "\x1B[0m" + `ray":[1,2,3,"123","456"]}}
	`)))
	res := string(Color([]byte(json), nil))
	res0 := strconv.Quote(res)
	if res0 != `"\x1b[1m{\x1b[0m\n  `+
		`\x1b[1m\x1b[94m\"hello\"\x1b[0m\x1b[1m:\x1b[0m `+
		`\x1b[32m\"world\"\x1b[0m\x1b[1m,\x1b[0m\n  `+
		`\x1b[1m\x1b[94m\"what\"\x1b[0m\x1b[1m:\x1b[0m `+
		`\x1b[33m123\x1b[0m\x1b[1m,\x1b[0m\n  `+
		`\x1b[1m\x1b[94m\"arr\"\x1b[0m\x1b[1m:\x1b[0m `+
		`\x1b[1m[\x1b[0m\x1b[32m\"1\"\x1b[0m, `+
		`\x1b[32m\"2\"\x1b[0m, \x1b[33m1\x1b[0m, \x1b[33m2\x1b[0m, `+
		`\x1b[36mtrue\x1b[0m, \x1b[36mfalse\x1b[0m, `+
		`\x1b[2mnull\x1b[0m\x1b[1m]\x1b[0m\x1b[1m,\x1b[0m\n  `+
		`\x1b[1m\x1b[94m\"obj\"\x1b[0m\x1b[1m:\x1b[0m \x1b[1m{\x1b[0m `+
		`\x1b[1m\x1b[94m\"key1\"\x1b[0m\x1b[1m:\x1b[0m `+
		`\x1b[2mnull\x1b[0m\x1b[1m,\x1b[0m `+
		`\x1b[1m\x1b[94m\"ar\\u001b[36mcyan\\u001b[0mray\"\x1b[0m\x1b[1m:\x1b[0m `+
		`\x1b[1m[\x1b[0m\x1b[33m1\x1b[0m, \x1b[33m2\x1b[0m, \x1b[33m3\x1b[0m, `+
		`\x1b[32m\"123\"\x1b[0m, \x1b[32m\"456\"\x1b[0m\x1b[1m]\x1b[0m `+
		`\x1b[1m}\x1b[0m\n\x1b[1m}\x1b[0m\n"` {
		t.Fatal("invalid output")
	}
}

func BenchmarkPretty(t *testing.B) {
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		Pretty(example1)
	}
}

func BenchmarkPrettySortKeys(t *testing.B) {
	opts := *DefaultOptions
	opts.SortKeys = true
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		PrettyOptions(example1, &opts)
	}
}

func BenchmarkUgly(t *testing.B) {
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		Ugly(example1)
	}
}

func BenchmarkUglyInPlace(t *testing.B) {
	example2 := []byte(string(example1))
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		UglyInPlace(example2)
	}
}

var example3 = []byte(`
{
  /* COMMENT 1 */
	"name": {
		"last": "Sanders", // outer 1
		"first": "Janet",  // outer 2
	}, 
  // COMMENT 2
	"children": [
		"Andy", "Carol", "Mike", // outer 3
	],
  /* 
  COMMENT 3
  */
	"values": [
		10.10, true, false, null, "hello", {},
	],
	"values2": {},
	"values3": [],
	"deep": {"deep":{"deep":[1,2,3,4,5,],}}
}
`)

func BenchmarkSpec(t *testing.B) {
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		Ugly(example3)
	}
}

func BenchmarkSpecInPlace(t *testing.B) {
	example4 := []byte(string(example3))
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		UglyInPlace(example4)
	}
}

func BenchmarkJSONIndent(t *testing.B) {
	var dst bytes.Buffer
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		json.Indent(&dst, example1, "", "  ")
	}
}

func BenchmarkJSONCompact(t *testing.B) {
	var dst bytes.Buffer
	t.ReportAllocs()
	t.ResetTimer()
	for i := 0; i < t.N; i++ {
		json.Compact(&dst, example1)
	}
}

func TestPrettyNoSpaceAfterNewline(t *testing.T) {
	json := `[{"foo":1,"bar":2},{"foo":3,"bar":4}]`
	json = string(Pretty([]byte(json)))
	if strings.Index(json, " \n") != -1 {
		t.Fatal("found a space followed by a newline, which should not be allowed")
	}
}

func TestPrettyStableSort(t *testing.T) {
	json := `{"c":3,"b":3,"a":3,"c":2,"b":2,"a":2,"c":1,"b":1,"a":1}`
	opts := *DefaultOptions
	opts.SortKeys = true
	json = string(Ugly(PrettyOptions([]byte(json), &opts)))
	if json != `{"a":1,"a":2,"a":3,"b":1,"b":2,"b":3,"c":1,"c":2,"c":3}` {
		t.Fatal("out of order")
	}
}

func TestPrettyColor(t *testing.T) {
	json := `"abc\u0020def\nghi"`
	ret := string(Color([]byte(json), nil))
	exp := "" +
		TerminalStyle.String[0] + `"abc` + TerminalStyle.String[1] +
		TerminalStyle.Escape[0] + `\u0020` + TerminalStyle.Escape[1] +
		TerminalStyle.String[0] + `def` + TerminalStyle.String[1] +
		TerminalStyle.Escape[0] + `\n` + TerminalStyle.Escape[1] +
		TerminalStyle.String[0] + `ghi"` + TerminalStyle.String[1]
	if ret != exp {
		t.Fatalf("expected '%s', got '%s'", exp, ret)
	}
}

func TestSpec(t *testing.T) {
	json := `
  {  //	hello
    "c": 3,"b":3, // jello
    /* SOME
       LIKE
       IT
       HAUT */
    "d": [ 1, /* 2 */ 3, 4, ],
  }`
	expect := `
  {    	     
    "c": 3,"b":3,         
           
           
         
              
    "d": [ 1,         3, 4  ] 
  }`
	out := string(Spec([]byte(json)))
	if out != expect {
		t.Fatalf("expected '%s', got '%s'", expect, out)
	}
	out = string(SpecInPlace([]byte(json)))
	if out != expect {
		t.Fatalf("expected '%s', got '%s'", expect, out)
	}
}

func TestStableSort10(t *testing.T) {
	expect := `{"key":"abc","key":"bbb","key":"rrr","key":"value","key3":3}`
	jsons := []string{
		`{"key3":3,"key":"abc","key":"value","key":"rrr","key":"bbb"}`,
		`{"key":"abc","key":"bbb","key":"value","key3":3,"key":"rrr"}`,
		`{"key":"bbb","key":"value","key":"rrr","key3":3,"key":"abc"}`,
		`{"key3":3,"key":"abc","key":"bbb","key":"value","key":"rrr"}`,
		`{"key3":3,"key":"abc","key":"bbb","key":"value","key":"rrr"}`,
	}
	opts := *DefaultOptions
	opts.SortKeys = true
	for _, json := range jsons {
		json = string(Ugly(PrettyOptions([]byte(json), &opts)))
		if json != expect {
			t.Fatalf("expected '%s', got '%s'", expect, json)
		}
	}
}

func TestNaN(t *testing.T) {
	vals := []string{"NaN", "nan", "Nan", "nAn", "inf", "Inf", "-inf", "+Inf"}
	for _, val := range vals {
		json := `{"num":` + val + `}`
		res := string(Ugly(Pretty([]byte(json))))
		if res != json {
			t.Fatalf("expected '%s', got '%s'", json, res)
		}
	}
}

// assertUglyInPlaceCompact 以标准库为参照, 检查原地压缩不会破坏转义或字符串内部空格.
func assertUglyInPlaceCompact(t *testing.T, js any) {
	t.Helper()
	src := fmt.Sprintf("%s", js)
	ugly := UglyInPlace([]byte(src))
	var buf bytes.Buffer
	err := json.Compact(&buf, []byte(src))
	if err != nil {
		panic(err)
	}
	if buf.String() != string(ugly) {
		t.Fatalf("for '%s', expected '%s', got '%s'", src, buf.Bytes(), ugly)
	}
}

// TestUglyInPlaceEscapedStrings 覆盖前导空白导致读写位置错开的转义边界.
func TestUglyInPlaceEscapedStrings(t *testing.T) {
	assertUglyInPlaceCompact(t, `  ["\\", "a b"]`)
	assertUglyInPlaceCompact(t, `  ["\\\\", "a b"]`)
	assertUglyInPlaceCompact(t, "\t\n"+`  ["\\\\", "a b"]`)
	assertUglyInPlaceCompact(t, ` ["a\" b", "c d"]`)
	assertUglyInPlaceCompact(t, ` ["\\\" x", "a b"]`)
	assertUglyInPlaceCompact(t, `   ["\\\" x", "a b"]`)
	assertUglyInPlaceCompact(t, `  {"a":"\\", "b":"c d"}`)
	assertUglyInPlaceCompact(t, ` {"\\":"a b", "nested": [{"quote":"\\\"", "text":"c d"}]}`)
	assertUglyInPlaceCompact(t, `  {"\\":"a b", "nested": [{"quote":"\\\"", "text":"c d"}]}`)
}

func TestIssue31(t *testing.T) {
	var s Style
	s.Brackets = [2]string{"0", "1"}
	s.String = [2]string{"2", "3"}
	s.Escape = [2]string{"4", "5"}
	r := string(Color([]byte(`["a\\a","a\\","\\a","\\","a\u0000a"]`), &s))
	e := `0[12"a34\\52a"3,2"a34\\52"3,2"34\\52a"3,2"34\\52"3,2"a34\u000052a"30]1`
	if r != e {
		t.Fatalf("expected\n'%s'\ngot\n'%s'", e, r)
	}
}

func TestSingleLineObjects(t *testing.T) {
	json := `{"shortArray": [1, 2, 3, 4],
	  "shortObject": {"code:": "some_code","value": 13262234
	  },
	  "shortNested": [{
	      "something": "any value","stuff": 12345}
	  ]
	}`
	json = string(Pretty([]byte(json)))
	if json != `{
  "shortArray": [1, 2, 3, 4],
  "shortObject": { "code:": "some_code", "value": 13262234 },
  "shortNested": [{ "something": "any value", "stuff": 12345 }]
}
` {
		t.Fatalf("invalid output")
	}
}

// TestSingleLineTrialAbort 覆盖本地修正: 单行试排超出宽度即提前放弃, 输出须与上游 v1.3.0 逐字节一致.
// 期望值由上游 v1.3.0 原版生成, 覆盖多层对象/数组嵌套、不同宽度与 SortKeys.
func TestSingleLineTrialAbort(t *testing.T) {
	cases := []struct {
		in    string
		width int
		sort  bool
		want  string
	}{
		{`{"zeta":{"b":[1,2,{"y":true,"x":null}],"a":"alpha"},"alpha":{"long_key_name_one":"value number one","long_key_name_two":{"inner_b":2,"inner_a":[{"k":"v"},{"k2":"v2"}]}},"m":[]}`, 30, false, `{
  "zeta": {
    "b": [
      1,
      2,
      { "y": true, "x": null }
    ],
    "a": "alpha"
  },
  "alpha": {
    "long_key_name_one": "value number one",
    "long_key_name_two": {
      "inner_b": 2,
      "inner_a": [
        { "k": "v" },
        { "k2": "v2" }
      ]
    }
  },
  "m": []
}
`},
		{`{"zeta":{"b":[1,2,{"y":true,"x":null}],"a":"alpha"},"alpha":{"long_key_name_one":"value number one","long_key_name_two":{"inner_b":2,"inner_a":[{"k":"v"},{"k2":"v2"}]}},"m":[]}`, 30, true, `{
  "alpha": {
    "long_key_name_one": "value number one",
    "long_key_name_two": {
      "inner_a": [
        { "k": "v" },
        { "k2": "v2" }
      ],
      "inner_b": 2
    }
  },
  "m": [],
  "zeta": {
    "a": "alpha",
    "b": [
      1,
      2,
      { "x": null, "y": true }
    ]
  }
}
`},
		{`{"zeta":{"b":[1,2,{"y":true,"x":null}],"a":"alpha"},"alpha":{"long_key_name_one":"value number one","long_key_name_two":{"inner_b":2,"inner_a":[{"k":"v"},{"k2":"v2"}]}},"m":[]}`, 80, false, `{
  "zeta": { "b": [1, 2, { "y": true, "x": null }], "a": "alpha" },
  "alpha": {
    "long_key_name_one": "value number one",
    "long_key_name_two": {
      "inner_b": 2,
      "inner_a": [{ "k": "v" }, { "k2": "v2" }]
    }
  },
  "m": []
}
`},
		{`{"zeta":{"b":[1,2,{"y":true,"x":null}],"a":"alpha"},"alpha":{"long_key_name_one":"value number one","long_key_name_two":{"inner_b":2,"inner_a":[{"k":"v"},{"k2":"v2"}]}},"m":[]}`, 80, true, `{
  "alpha": {
    "long_key_name_one": "value number one",
    "long_key_name_two": {
      "inner_a": [{ "k": "v" }, { "k2": "v2" }],
      "inner_b": 2
    }
  },
  "m": [],
  "zeta": { "a": "alpha", "b": [1, 2, { "x": null, "y": true }] }
}
`},
		{`[{"name":"svc-a","ports":[80,443],"env":{"MODE":"prod","LEVEL":"info"}},{"name":"svc-b","ports":[8080],"env":{}}]`, 30, false, `[
  {
    "name": "svc-a",
    "ports": [80, 443],
    "env": {
      "MODE": "prod",
      "LEVEL": "info"
    }
  },
  {
    "name": "svc-b",
    "ports": [8080],
    "env": { }
  }
]
`},
		{`[{"name":"svc-a","ports":[80,443],"env":{"MODE":"prod","LEVEL":"info"}},{"name":"svc-b","ports":[8080],"env":{}}]`, 30, true, `[
  {
    "env": {
      "LEVEL": "info",
      "MODE": "prod"
    },
    "name": "svc-a",
    "ports": [80, 443]
  },
  {
    "env": { },
    "name": "svc-b",
    "ports": [8080]
  }
]
`},
		{`[{"name":"svc-a","ports":[80,443],"env":{"MODE":"prod","LEVEL":"info"}},{"name":"svc-b","ports":[8080],"env":{}}]`, 80, false, `[
  {
    "name": "svc-a",
    "ports": [80, 443],
    "env": { "MODE": "prod", "LEVEL": "info" }
  },
  { "name": "svc-b", "ports": [8080], "env": { } }
]
`},
		{`[{"name":"svc-a","ports":[80,443],"env":{"MODE":"prod","LEVEL":"info"}},{"name":"svc-b","ports":[8080],"env":{}}]`, 80, true, `[
  {
    "env": { "LEVEL": "info", "MODE": "prod" },
    "name": "svc-a",
    "ports": [80, 443]
  },
  { "env": { }, "name": "svc-b", "ports": [8080] }
]
`},
	}
	for _, c := range cases {
		got := string(PrettyOptions([]byte(c.in), &Options{Width: c.width, Indent: "  ", SortKeys: c.sort}))
		if got != c.want {
			t.Fatalf("width=%d sort=%v\ngot:\n%s\nwant:\n%s", c.width, c.sort, got, c.want)
		}
	}
}

// nestedObjectJSON 生成 depth 层、每层 width 个键的嵌套对象, 叶子为短字符串.
func nestedObjectJSON(depth, width int) []byte {
	var b bytes.Buffer
	var gen func(d int)
	gen = func(d int) {
		if d == 0 {
			b.WriteString(`"leaf value"`)
			return
		}
		b.WriteByte('{')
		for i := range width {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"key_%d_%d":`, width-i, d)
			gen(d - 1)
		}
		b.WriteByte('}')
	}
	gen(depth)
	return b.Bytes()
}

// BenchmarkPrettyNested 衡量多层嵌套对象的格式化开销, 用于观察单行试排失败后的回退成本.
func BenchmarkPrettyNested(b *testing.B) {
	doc := nestedObjectJSON(5, 5)
	for _, sk := range []bool{false, true} {
		b.Run(fmt.Sprintf("sort=%v", sk), func(b *testing.B) {
			opts := &Options{Width: 80, Indent: "  ", SortKeys: sk}
			b.ReportAllocs()
			for b.Loop() {
				PrettyOptions(doc, opts)
			}
		})
	}
}

// uglyReference 是只读快进优化前的逐字节实现, 仅作差分测试的参照, 不得随生产实现修改.
func uglyReference(dst, src []byte) []byte {
	dst = dst[:0]
	for i := 0; i < len(src); i++ {
		if src[i] > ' ' {
			dst = append(dst, src[i])
			if src[i] == '"' {
				for i = i + 1; i < len(src); i++ {
					dst = append(dst, src[i])
					if src[i] == '"' {
						j := len(dst) - 2
						for ; dst[j] == '\\'; j-- {
						}
						if (j-(len(dst)-1))%2 != 0 {
							break
						}
					}
				}
			}
		}
	}
	return dst
}

// checkUglyAgainstReference 对同一输入核对 Ugly 和 UglyInPlace 与参照实现逐字节一致, 并检查两者的所有权契约:
// Ugly 不改输入且返回独立副本, 容量等于输入长度; UglyInPlace 复用输入底层数组, 容量不变.
func checkUglyAgainstReference(t *testing.T, in []byte) {
	t.Helper()
	want := uglyReference(make([]byte, 0, len(in)), in)
	orig := string(in)

	got := Ugly(in)
	if string(got) != string(want) {
		t.Fatalf("Ugly(%q) = %q, want %q", orig, got, want)
	}
	if string(in) != orig {
		t.Fatalf("Ugly modified input %q to %q", orig, in)
	}
	if got == nil || cap(got) != len(in) {
		t.Fatalf("Ugly(%q) cap = %d (nil=%v), want non-nil with cap %d", orig, cap(got), got == nil, len(in))
	}
	if len(got) > 0 {
		// 改写返回值首字节, 输入不应随之变化, 证明两者不共用底层数组.
		got[0] ^= 0xff
		if string(in) != orig {
			t.Fatalf("Ugly(%q) result aliases input", orig)
		}
		got[0] ^= 0xff
	}

	buf := []byte(orig)
	gotIn := UglyInPlace(buf)
	if string(gotIn) != string(want) {
		t.Fatalf("UglyInPlace(%q) = %q, want %q", orig, gotIn, want)
	}
	if cap(gotIn) != cap(buf) || (len(gotIn) > 0 && &gotIn[0] != &buf[0]) {
		t.Fatalf("UglyInPlace(%q) did not reuse input buffer", orig)
	}
	if len(gotIn) == len(buf) && string(buf) != orig {
		t.Fatalf("UglyInPlace(%q) without removable bytes rewrote input to %q", orig, buf)
	}
}

// TestUglyBoundaryCases 覆盖快进切换点, 16 字节逐字节扫描与 IndexByte 跳转的交界, 奇偶反斜杠和未闭合字符串.
func TestUglyBoundaryCases(t *testing.T) {
	long := strings.Repeat("x", 40)
	cases := []string{
		"", " ", "\x00", `"`, `""`, `"\`, `"\\`, `"\"`, `"\\"`, ` "\"`, ` "\\" `,
		`{"a":1}`, ` {"a":1}`, `{"a":1} `, `{"a" :1}`, `{"a b":1}`, "{\"a\":\t1}",
		`["\\", "a b"]`, `  ["\\\\", "a b"]`, ` ["\\\" x", "a b"]`,
		`"` + long + `"`, `"` + long + `" `, ` "` + long + `" `,
		`"` + long + `\"` + long + `" `, `"` + long + `\\"` + long + `" `,
		`"` + long + `\\\"` + long + `" `, `"` + long + `\`, `"` + long + `\\`,
		`"` + long + `\" `, `"` + long + `\\" `, ` "` + long + `\\\" x`,
		`"` + strings.Repeat(`\`, 33) + `" , "a"`, `"` + strings.Repeat(`\`, 34) + `" , "a"`,
		`{"k":"中文 value","v":"` + long + ` tail"}` + "\n",
	}
	// 引号恰好落在逐字节扫描上限前后的各个位置, 前置不同数量反斜杠.
	for n := 12; n <= 20; n++ {
		for bs := 0; bs <= 3; bs++ {
			body := strings.Repeat("y", n) + strings.Repeat(`\`, bs)
			cases = append(
				cases,
				`"`+body+`" , "z z"`,
				` "`+body+`" , "z z"`,
				`"`+body+`"`+`"`+long+`" `,
			)
		}
	}
	for _, c := range cases {
		checkUglyAgainstReference(t, []byte(c))
	}
	// nil 输入与旧实现一致: Ugly 返回非 nil 空切片, UglyInPlace 原样返回 nil.
	if got := Ugly(nil); got == nil || len(got) != 0 || cap(got) != 0 {
		t.Fatalf("Ugly(nil) = %#v (cap %d), want non-nil empty slice", got, cap(got))
	}
	if got, want := UglyInPlace(nil), uglyReference(nil, nil); got != nil || want != nil {
		t.Fatalf("UglyInPlace(nil) = %#v, reference %#v, want both nil", got, want)
	}
}

// TestUglyMatchesReferenceRandom 用固定种子生成偏向引号, 反斜杠, 空白和长字符串的随机输入, 与参照实现差分.
func TestUglyMatchesReferenceRandom(t *testing.T) {
	alphabet := []byte("\"\"\"\\\\  \t\n\r\x00\x1f{}[]:,a1\x80\xe4")
	rng := rand.New(rand.NewSource(20261116))
	rounds := 300000
	if testing.Short() {
		rounds = 30000
	}
	buf := make([]byte, 0, 256)
	for range rounds {
		buf = buf[:0]
		n := rng.Intn(96)
		for len(buf) < n {
			switch r := rng.Intn(20); {
			case r == 0:
				// 长于 16 字节的无引号片段, 驱动 IndexByte 跳转路径.
				buf = append(buf, strings.Repeat("s", 10+rng.Intn(40))...)
			case r == 1:
				buf = append(buf, strings.Repeat(`\`, 1+rng.Intn(6))...)
			default:
				buf = append(buf, alphabet[rng.Intn(len(alphabet))])
			}
		}
		checkUglyAgainstReference(t, buf)
	}
}

// FuzzUgly 持续与参照实现差分; 普通 go test 只运行种子语料.
func FuzzUgly(f *testing.F) {
	for _, s := range []string{
		"", `{"a":"b c"}`, ` {"a":1}`, `"` + strings.Repeat("x", 20) + `\" "`,
		`"\\\\"  `, `"unterminated \`, string(example1), string(example3),
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		checkUglyAgainstReference(t, in)
	})
}

// uglyBenchShapes 构造约 1.5MiB 的五类批量正文和约 600 字节的单条上报, 覆盖快进收益最大到最小的形态.
func uglyBenchShapes() []struct {
	name string
	data []byte
} {
	const record = `{"brand":"example","x_event_id":"http_error","uid":123456789,"ver":"5.6.7.8","os":"windows",` +
		`"ip":"192.0.2.10","isp":"电信","province":"广东","city":"深圳","game":"some-game-name","node":"node-12",` +
		`"rtt":35.5,"loss":0.01,"jitter":2,"ok":true,"msg":"line1\nline2 \"quoted\"","tags":["a","b","c"],` +
		`"data":{"node_line_type":1,"brand":"example"},"ext":{"k1":"v1","k2":2,"k3":[1,2,3]},` +
		`"ua":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko)",` +
		`"_ctime":"2026-11-14T08:00:00Z","tail":"end"}`
	const size = 3 << 19
	batch := func(item string) []byte {
		var b bytes.Buffer
		b.WriteByte('[')
		for b.Len() < size {
			if b.Len() > 1 {
				b.WriteByte(',')
			}
			b.WriteString(item)
		}
		b.WriteByte(']')
		return b.Bytes()
	}
	noSpace := strings.NewReplacer(" ", "_").Replace(record)
	short := `{"a":"b","c":"d","e":"f","g":1,"h":"i","j":["k","l","m"],"n":{"o":"p"}}`
	// 长字符串记录: 约 300 字节的消息和 URL, 观察收益随字符串长度增长的趋势.
	long := `{"id":123456,"level":"error","msg":"` + strings.Repeat(`request failed: upstream \"api\" timeout after 3 retries; `, 5) +
		`","url":"https://example.com/v1/items?` + strings.Repeat("key=value&", 20) + `end=1"}`
	return []struct {
		name string
		data []byte
	}{
		{"batch-nospace", batch(noSpace)},
		{"batch-strspace", batch(record)},
		{"batch-shortstr", batch(short)},
		{"batch-longstr", batch(long)},
		{"batch-indented", Pretty(batch(record))},
		{"record-compact", []byte(record)},
		{"record-indented", Pretty([]byte(record))},
	}
}

// BenchmarkUglyShapes 衡量各形态的 Ugly 和 UglyInPlace; 原地版本每轮先恢复输入, 计时包含这次复制.
func BenchmarkUglyShapes(b *testing.B) {
	for _, s := range uglyBenchShapes() {
		b.Run(s.name+"/copy", func(b *testing.B) {
			b.SetBytes(int64(len(s.data)))
			b.ReportAllocs()
			for b.Loop() {
				Ugly(s.data)
			}
		})
		b.Run(s.name+"/inplace", func(b *testing.B) {
			work := make([]byte, len(s.data))
			b.SetBytes(int64(len(s.data)))
			b.ReportAllocs()
			for b.Loop() {
				copy(work, s.data)
				UglyInPlace(work)
			}
		})
	}
}
