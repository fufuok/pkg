package master

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

func TestWaitUntilNtpdate(t *testing.T) {
	preserveMasterPackageState(t)

	assert.False(t, WaitUntilNtpdate(10*time.Millisecond))
	close(ntpFirstDoneChan)
	assert.True(t, WaitUntilNtpdate(10*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(50*time.Millisecond))
}

// TestStartTimeSyncRestartAfterDisableDoesNotPanic 验证 TimeSyncType 关后再开不会二次 close.
// NodeAgent / DataRouter / DataPlugins 热更新走 runtimeTimeSync, 重启时会换新的首次完成 channel.
func TestStartTimeSyncRestartAfterDisableDoesNotPanic(t *testing.T) {
	preserveClockOffsetChan(t)
	oldMin := common.ClockOffsetMinInterval
	oldInterval := common.ClockOffsetInterval
	t.Cleanup(func() {
		common.ClockOffsetMinInterval = oldMin
		common.ClockOffsetInterval = oldInterval
	})
	common.ClockOffsetMinInterval = 5 * time.Millisecond
	common.ClockOffsetInterval = 20 * time.Millisecond

	var calls atomic.Int32
	clockOffsetChanOf = func(ctx context.Context, dur time.Duration) (string, chan time.Duration) {
		_ = dur
		calls.Add(1)
		setNTPName("default")
		ch := make(chan time.Duration, 1)
		ch <- 3 * time.Millisecond
		// 保持通道打开直到 ctx 取消, 模拟生产周期同步不会立刻退出.
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return "default", ch
	}

	cfg := prepareMasterConfig(t)
	if config.Config() == nil {
		t.Fatal("prepareMasterConfig left Config nil")
	}
	cfg.SYSConf.TimeSyncType = "default"
	assert.Nil(t, startTimeSync())
	if !WaitUntilNtpdate(time.Second) {
		t.Fatalf("first NTP sync did not complete, factory calls=%d", calls.Load())
	}

	cfg.SYSConf.TimeSyncType = ""
	assert.Nil(t, runtimeTimeSync())
	cfg.SYSConf.TimeSyncType = "default"
	assert.Nil(t, runtimeTimeSync())
	if !WaitUntilNtpdate(time.Second) {
		t.Fatalf("restarted NTP sync did not complete, factory calls=%d", calls.Load())
	}
}

// preserveClockOffsetChan 保存时钟通道工厂并在测试结束时恢复.
func preserveClockOffsetChan(t *testing.T) {
	t.Helper()
	old := clockOffsetChanOf
	t.Cleanup(func() {
		clockOffsetChanOf = old
	})
}
