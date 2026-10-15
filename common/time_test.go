package common

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/utils"
)

const testTimeLocationEnv = "PKG_TEST_STARTUP_TIME_LOCATION"

// TestInitTimeLocation 用子进程隔离不可重置的启动状态和标准库 Local.
// 父进程不写全局时区; Linux 使用 UTC 宿主, Windows 保留真实宿主规则.
func TestInitTimeLocation(t *testing.T) {
	if mode := os.Getenv(testTimeLocationEnv); mode != "" {
		testTimeLocationChild(t, mode)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	for _, mode := range []string{"default", "new-york", "utc", "system", "nil", "closed-pool"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestInitTimeLocation$", "-test.count=1")
			cmd.Env = append(os.Environ(), "TZ=UTC", testTimeLocationEnv+"="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("time location subprocess: %v\n%s", err, output)
			}
		})
	}
}

// testTimeLocationChild 覆盖选择前校验、一次性应用以及停池失败的边界.
func testTimeLocationChild(t *testing.T, mode string) {
	t.Helper()
	host := time.Local
	start := StartTime
	if host == config.DefaultTimeLocation {
		t.Fatal("config import changed the standard library local time location")
	}
	selectTestTimeLocation(t, mode)
	if mode == "nil" {
		if err := InitTimeLocation(); err == nil || time.Local != host || ants.IsClosed() {
			t.Fatalf("nil location changed startup state: %v", err)
		}
		config.DefaultTimeLocation = utils.ChinaTimeLocation
	}
	if mode == "closed-pool" {
		if err := ants.ReleaseTimeout(5 * time.Second); err != nil {
			t.Fatalf("close startup pool: %v", err)
		}
		for range 2 {
			if err := InitTimeLocation(); !errors.Is(err, ants.ErrPoolClosed) {
				t.Fatalf("closed pool error = %v", err)
			}
		}
		if time.Local != host || StartTime != start || config.DefaultTimeZone != utils.ChinaTimeZone {
			t.Fatal("failed initialization changed application time state")
		}
		return
	}

	// 让已导入的默认池至少经历一次 tick, 避免只验证后台尚未读取的偶然时序.
	if mode == "default" || mode == "new-york" {
		time.Sleep(600 * time.Millisecond)
	}
	capacity := ants.Cap()
	if err := InitTimeLocation(); err != nil {
		t.Fatalf("initialize application time location: %v", err)
	}
	loc := config.DefaultTimeLocation
	if time.Local != loc || config.DefaultTimeZone != loc.String() {
		t.Fatal("standard library and application locations differ")
	}
	// 完整比较包含单调时钟, 避免只用 Equal 漏掉 In 转换造成的单调时钟丢失.
	if StartTime != start {
		t.Fatal("startup time changed, including its monotonic clock reading")
	}
	if ants.IsClosed() || ants.Cap() != capacity {
		t.Fatal("startup pool was not restored")
	}
	checkSelectedTimeLocation(t, mode, loc)
	checkApplicationClock(t, loc)
	checkTimeLocationReentry(t, loc)
}

// selectTestTimeLocation 仅在应用时区初始化前设置输入, 不修改 Local.
func selectTestTimeLocation(t *testing.T, mode string) {
	t.Helper()
	switch mode {
	case "new-york":
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Fatalf("load New York time location: %v", err)
		}
		config.DefaultTimeLocation = loc
	case "utc":
		config.DefaultTimeLocation = time.UTC
	case "system":
		config.DefaultTimeLocation = time.Local
	case "nil":
		config.DefaultTimeLocation = nil
	case "default", "closed-pool":
	default:
		t.Fatalf("unknown subprocess mode %q", mode)
	}
}

// checkSelectedTimeLocation 同时验证固定偏移、纽约夏令时和零值哨兵语义.
func checkSelectedTimeLocation(t *testing.T, mode string, loc *time.Location) {
	t.Helper()
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC).In(time.Local)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC).In(time.Local)
	_, winterOffset := winter.Zone()
	_, summerOffset := summer.Zone()
	switch mode {
	case "new-york":
		if winterOffset != -5*60*60 || summerOffset != -4*60*60 {
			t.Fatalf("New York winter/summer offsets = %d/%d", winterOffset, summerOffset)
		}
	case "default", "nil":
		if winterOffset != utils.ChinaTimeOffset || summerOffset != utils.ChinaTimeOffset {
			t.Fatalf("default winter/summer offsets = %d/%d", winterOffset, summerOffset)
		}
	case "utc":
		if winterOffset != 0 || summerOffset != 0 {
			t.Fatalf("UTC winter/summer offsets = %d/%d", winterOffset, summerOffset)
		}
	case "system":
	}
	if time.Now().Location() != loc {
		t.Fatal("time.Now does not use the selected location")
	}
	if config.ZeroTimeCST.Format(time.RFC3339) != "0001-01-01T00:00:00+08:00" ||
		config.ZeroTimeCST.IsZero() || !config.ZeroTimeUTC.IsZero() {
		t.Fatal("zero time sentinel semantics changed")
	}
}

// checkApplicationClock 验证选择时区后仍保留既有 NTP 偏移路径.
func checkApplicationClock(t *testing.T, loc *time.Location) {
	t.Helper()
	SetClockOffset(2 * time.Hour)
	offset := GetClockOffset()
	before := time.Now().Add(offset)
	now := GTimeNow()
	after := time.Now().Add(offset)
	if now.Location() != loc || offset == 0 || now.Before(before) || now.After(after) {
		t.Fatal("business clock lost the selected location or clock offset")
	}
}

// checkTimeLocationReentry 验证池恢复可用、同值重入不再写全局和异值切换被拒绝.
func checkTimeLocationReentry(t *testing.T, loc *time.Location) {
	t.Helper()
	done := make(chan bool, 1)
	if err := ants.Submit(func() {
		for range 1000 {
			if time.Now().Location() != loc {
				done <- false
				return
			}
		}
		done <- true
	}); err != nil {
		t.Fatalf("submit after timezone initialization: %v", err)
	}
	if err := InitTimeLocation(); err != nil {
		t.Fatalf("repeat timezone initialization: %v", err)
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("worker observed a different location")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("restored pool did not execute a task")
	}
	config.DefaultTimeLocation = time.FixedZone("different", 0)
	if err := InitTimeLocation(); err == nil || time.Local != loc {
		t.Fatalf("late location replacement was not rejected: %v", err)
	}
	config.DefaultTimeLocation = loc
}
