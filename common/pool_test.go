package common

import (
	"runtime"
	"testing"

	"github.com/fufuok/pkg/config"
)

// TestPoolShardsFallsBackToRuntime 验证默认协程池分片数在未覆盖时回退到运行时值.
//
// DefaultGOMAXPROCS 小于等于 0 表示不覆盖, 分片数必须仍为正数, 避免 MaxGoPool/size 除零.
func TestPoolShardsFallsBackToRuntime(t *testing.T) {
	if got := poolShards(3); got != 3 {
		t.Fatalf("poolShards(3) = %d, want 3", got)
	}
	want := runtime.GOMAXPROCS(0)
	for _, n := range []int{0, -2} {
		if got := poolShards(n); got != want || got < 1 {
			t.Fatalf("poolShards(%d) = %d, want runtime GOMAXPROCS %d", n, got, want)
		}
	}
}

// TestPoolShardsHonorsPositiveConfiguration 验证默认值也是有效正数配置, 不被当前运行时替换.
func TestPoolShardsHonorsPositiveConfiguration(t *testing.T) {
	configured := config.DefaultGOMAXPROCS
	previous := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	runtime.GOMAXPROCS(configured + 1)
	if got := poolShards(configured); got != configured {
		t.Fatalf("pool shards = %d, want configured value %d", got, configured)
	}
}
