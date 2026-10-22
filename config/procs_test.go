package config

import (
	"os"
	"runtime"
	"strconv"
	"testing"
)

// TestDefaultGOMAXPROCSFollowsRuntime 验证默认值取自运行时, 不再强制提升到至少 4.
//
// 以 GOMAXPROCS=1 运行测试时默认值必须为 1, 证明环境变量与 cgroup 限额不再被覆盖.
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
