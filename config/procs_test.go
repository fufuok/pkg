package config

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

// TestDefaultGOMAXPROCSFollowsRuntime 验证初始默认值取自运行时, 不再强制至少为 4.
//
// 以 GOMAXPROCS=1 运行时默认值必须为 1; 本用例不模拟 cgroup 配额变化.
func TestDefaultGOMAXPROCSFollowsRuntime(t *testing.T) {
	if DefaultGOMAXPROCS != runtime.GOMAXPROCS(0) {
		t.Fatalf("DefaultGOMAXPROCS = %d, want runtime GOMAXPROCS %d", DefaultGOMAXPROCS, runtime.GOMAXPROCS(0))
	}
	if env := os.Getenv("GOMAXPROCS"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n > 0 && DefaultGOMAXPROCS != n {
			t.Fatalf("DefaultGOMAXPROCS = %d, want GOMAXPROCS env %d", DefaultGOMAXPROCS, n)
		}
	}
}
