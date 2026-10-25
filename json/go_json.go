//go:build !std_json

package json

import (
	"github.com/goccy/go-json"
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
	Valid = json.Valid
)
