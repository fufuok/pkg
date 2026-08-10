//go:build !std_json

package json

import "testing"

// TestGoJSONNameContract 验证默认构建明确暴露 go_json 实现标识.
func TestGoJSONNameContract(t *testing.T) {
	if Name != "go_json" {
		t.Fatalf("Name = %q, want go_json", Name)
	}
}
