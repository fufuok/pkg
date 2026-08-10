package stats

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fufuok/ants"
)

// TestMain 在 stats 测试进程退出前等待 ants 默认池后台 goroutine 结束.
// stats 会读取协程池指标, Linux race 退出阶段必须显式释放默认池.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := ants.ReleaseTimeout(5 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "release ants default pool: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
