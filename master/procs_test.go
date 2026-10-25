package master

import (
	"runtime"
	"testing"

	"github.com/fufuok/pkg/config"
)

// TestApplyGOMAXPROCSOnlyOverridesDifferentPositiveValue 验证只有正数且不同于当前值时覆盖.
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
	configured := cur + 1
	if !applyGOMAXPROCS(configured) {
		t.Fatalf("applyGOMAXPROCS(%d) skipped, want applied", configured)
	}
	if got := runtime.GOMAXPROCS(0); got != configured {
		t.Fatalf("GOMAXPROCS = %d, want %d", got, configured)
	}
}

// TestApplyGOMAXPROCSHonorsPositiveConfiguration 验证正数配置不因等于启动默认值而失效.
// 人工改变当前运行时后仍须使用配置值, 不通过数值相等关系猜测应用是否赋值.
func TestApplyGOMAXPROCSHonorsPositiveConfiguration(t *testing.T) {
	configured := config.DefaultGOMAXPROCS
	previous := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	runtime.GOMAXPROCS(configured + 1)
	if !applyGOMAXPROCS(configured) {
		t.Fatal("positive configuration matching the startup default was ignored")
	}
	if got := runtime.GOMAXPROCS(0); got != configured {
		t.Fatalf("GOMAXPROCS = %d, want configured value %d", got, configured)
	}
}
