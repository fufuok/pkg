package master

import (
	"context"
	"sync"
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

// TestStartTimeSyncContinuesAfterInitialEmptyChannel 首次通道无样本时仍必须进入周期同步.
func TestStartTimeSyncContinuesAfterInitialEmptyChannel(t *testing.T) {
	preserveClockOffsetChan(t)
	oldMin := common.ClockOffsetMinInterval
	oldInterval := common.ClockOffsetInterval
	t.Cleanup(func() {
		common.ClockOffsetMinInterval = oldMin
		common.ClockOffsetInterval = oldInterval
	})
	common.ClockOffsetMinInterval = 5 * time.Millisecond
	common.ClockOffsetInterval = 20 * time.Millisecond

	var firstCalls, periodicCalls atomic.Int32
	clockOffsetChanOf = func(ctx context.Context, dur time.Duration) (string, chan time.Duration) {
		if dur == common.ClockOffsetMinInterval {
			firstCalls.Add(1)
			ch := make(chan time.Duration)
			close(ch)
			return "default", ch
		}
		periodicCalls.Add(1)
		ch := make(chan time.Duration, 1)
		ch <- 2 * time.Second
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return "default", ch
	}

	oldOffset := common.GetClockOffset()
	t.Cleanup(func() {
		common.SetClockOffset(oldOffset)
	})

	cfg := prepareMasterConfig(t)
	cfg.SYSConf.TimeSyncType = "default"
	assert.Nil(t, startTimeSync())
	if !WaitUntilNtpdate(time.Second) {
		t.Fatalf("periodic NTP sync did not complete, first=%d periodic=%d", firstCalls.Load(), periodicCalls.Load())
	}
	if firstCalls.Load() < 1 || periodicCalls.Load() < 1 {
		t.Fatalf("factory calls first=%d periodic=%d, want both >= 1", firstCalls.Load(), periodicCalls.Load())
	}

	assert.Nil(t, runtimeTimeSync())
	if firstCalls.Load() != 1 || periodicCalls.Load() != 1 {
		t.Fatalf("same-type reload restarted a live session, first=%d periodic=%d", firstCalls.Load(), periodicCalls.Load())
	}
}

// TestStopTimeSyncDropsStaleOffset 已取消会话不得在 Stop 清零后再写 clockOffset.
func TestStopTimeSyncDropsStaleOffset(t *testing.T) {
	preserveClockOffsetChan(t)
	oldMin := common.ClockOffsetMinInterval
	oldInterval := common.ClockOffsetInterval
	t.Cleanup(func() {
		common.ClockOffsetMinInterval = oldMin
		common.ClockOffsetInterval = oldInterval
	})
	common.ClockOffsetMinInterval = 5 * time.Millisecond
	common.ClockOffsetInterval = 20 * time.Millisecond

	releaseStale := make(chan struct{})
	staleSent := make(chan struct{})
	// periodicReady 标记周期 factory 已建立. 若在此之前 Stop, ntpdate 会因 parent 已取消直接返回,
	// 旧样本发送路径根本不会挂上, 测试会误报 "stale sample was not delivered".
	periodicReady := make(chan struct{})
	var periodicReadyOnce sync.Once
	clockOffsetChanOf = func(ctx context.Context, dur time.Duration) (string, chan time.Duration) {
		ch := make(chan time.Duration, 1)
		if dur == common.ClockOffsetMinInterval {
			ch <- 2 * time.Hour
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return "default", ch
		}
		periodicReadyOnce.Do(func() {
			close(periodicReady)
		})
		go func() {
			<-releaseStale
			ch <- 3 * time.Hour
			close(staleSent)
			close(ch)
		}()
		return "default", ch
	}

	oldOffset := common.GetClockOffset()
	t.Cleanup(func() {
		common.SetClockOffset(oldOffset)
	})

	cfg := prepareMasterConfig(t)
	cfg.SYSConf.TimeSyncType = "default"
	assert.Nil(t, startTimeSync())
	if !WaitUntilNtpdate(time.Second) {
		t.Fatal("first NTP sync did not complete")
	}
	select {
	case <-periodicReady:
	case <-time.After(time.Second):
		t.Fatal("periodic NTP factory was not established")
	}
	ntpMu.Lock()
	done := ntpDone
	ntpMu.Unlock()
	assert.Nil(t, stopTimeSync())
	afterStop := common.GetClockOffset()
	if afterStop >= time.Hour {
		t.Fatalf("clock offset after stop = %s, want a cleared value", afterStop)
	}

	close(releaseStale)
	select {
	case <-staleSent:
	case <-time.After(time.Second):
		t.Fatal("stale NTP sample was not delivered")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stale NTP session did not exit")
	}
	if got := common.GetClockOffset(); got >= time.Hour {
		t.Fatalf("stale NTP session overwrote clock offset = %s", got)
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
