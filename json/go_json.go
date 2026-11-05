//go:build !std_json

package json

import (
	stdjson "encoding/json"

	"github.com/goccy/go-json"
)

const (
	// maxPooledValidBytes 限制 goccy/go-json.Valid 单次复制到内部 sync.Pool 的最大字节数 (4 MiB).
	// goccy 会在池化上下文中拷贝整份输入且归还时不缩容; 超大输入改走标准库零拷贝状态机, 防止偶发大包常驻堆内存.
	maxPooledValidBytes = 4 << 20
)

var (
	Name          = "go_json"
	Marshal       = json.Marshal
	Unmarshal     = json.Unmarshal
	MarshalIndent = json.MarshalIndent
	NewDecoder    = json.NewDecoder
	NewEncoder    = json.NewEncoder
	// Valid 只做语法校验: 恰好一个 JSON 值, 允许首尾空白, 嵌套上限 10000 层;
	// 不检查字符串内的 UTF-8 合法性, 也不拒绝重复字段, 两个构建版本结论一致.
	Valid = validGoJSON
)

// validGoJSON 在常规尺寸下复用 goccy/go-json 的高速语法扫描, 超过池安全阈值时回退标准库零拷贝校验.
// 两条路径遵循完全一致的 JSON v1 语法与 10000 层嵌套上限契约.
func validGoJSON(data []byte) bool {
	if len(data) > maxPooledValidBytes {
		return stdjson.Valid(data)
	}
	return json.Valid(data)
}
