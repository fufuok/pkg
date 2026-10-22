package master

import (
	"runtime"
	"testing"
)

// TestApplyGOMAXPROCSOnlyOverridesDifferentPositiveValue 验证只有正数且不同于当前值时才调用运行时设置.
//
// 默认值等于当前值时若仍调用 runtime.GOMAXPROCS, Go 1.25+ 会关闭随 cgroup 限额的自动更新,
// 因此跳过分支是 RP-6 的核心契约.
func TestApplyGOMAXPROCSOnlyOverridesDifferentPositiveValue(t *testing.T) {
	cur := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(cur) })

	for _, n := range []int{0, -1, cur} {
		if applyGOMAXPROCS(n) {
			t.Fatalf("applyGOMAXPROCS(%d) applied, want skipped (current %d)", n, cur)
		}
		if got := runtime.GOMAXPROCS(0); got != cur {
			t.Fatalf("GOMAXPROCS = %d after applyGOMAXPROCS(%d), want %d", got, n, cur)
		}
	}
	if !applyGOMAXPROCS(cur + 1) {
		t.Fatalf("applyGOMAXPROCS(%d) skipped, want applied", cur+1)
	}
	if got := runtime.GOMAXPROCS(0); got != cur+1 {
		t.Fatalf("GOMAXPROCS = %d, want %d", got, cur+1)
	}
}
