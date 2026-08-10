//go:build std_json

package json

import "testing"

// TestStdJSONNameContract 验证 std_json 构建明确暴露标准库实现标识.
func TestStdJSONNameContract(t *testing.T) {
	if Name != "std_json" {
		t.Fatalf("Name = %q, want std_json", Name)
	}
}
