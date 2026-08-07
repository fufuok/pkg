package master

import (
	"os"
	"testing"
	"time"

	"github.com/fufuok/ants"
)

// TestMain 在测试进程退出前等待由 common 依赖引入的 ants 默认池结束.
//
// 该清理只作用于测试二进制, 不改变 master 的生产生命周期和停止顺序.
func TestMain(m *testing.M) {
	code := m.Run()
	_ = ants.ReleaseTimeout(5 * time.Second)
	os.Exit(code)
}
