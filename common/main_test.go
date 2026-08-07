package common

import (
	"os"
	"testing"
	"time"

	"github.com/fufuok/ants"
)

// TestMain 在测试进程退出前等待 ants 默认池的后台任务结束.
//
// race 运行器默认会在退出阶段额外等待后台 goroutine. 若默认池仍在运行,
// Linux 测试进程可能在 PASS 后因退出期资源竞争收到 SIGSEGV.
func TestMain(m *testing.M) {
	code := m.Run()
	_ = ants.ReleaseTimeout(5 * time.Second)
	os.Exit(code)
}
