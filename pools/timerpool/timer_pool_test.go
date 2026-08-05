package timerpool

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

const timerTestTimeout = 2 * time.Second

// waitTimer 等待 timer 触发, 并用独立超时阻止实现错误导致测试永久阻塞.
// 超时只作为失败上限, 不参与正常触发时间的精确断言.
func waitTimer(t *testing.T, timer *time.Timer) time.Time {
	t.Helper()
	select {
	case fired := <-timer.C:
		return fired
	case <-time.After(timerTestTimeout):
		t.Fatal("pooled timer did not fire before test timeout")
		return time.Time{}
	}
}

// TestReleaseAndReuse 验证活动、已触发和已停止 timer 都能安全归还并重新用于新期限.
// 断言使用 timer 通道携带的单调时间识别陈旧事件, 不要求 sync.Pool 复用具体指针.
func TestReleaseAndReuse(t *testing.T) {
	const reuseDelay = 10 * time.Millisecond
	tests := []struct {
		name  string
		state string
	}{
		{name: "active timer", state: "active"},
		{name: "fired timer", state: "fired"},
		{name: "stopped timer", state: "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timer := New(time.Hour)
			switch tt.state {
			case "fired":
				assert.True(t, timer.Reset(0))
				_ = waitTimer(t, timer)
			case "stopped":
				assert.True(t, timer.Stop())
			}
			Release(timer)

			start := time.Now()
			reused := New(reuseDelay)
			fired := waitTimer(t, reused)
			assert.False(t, fired.Before(start.Add(reuseDelay)), "timer delivered a stale event")
			Release(reused)
		})
	}
}

// TestConcurrentNewAndRelease 验证高并发定时器复用不会串扰截止时间或触发数据竞争.
// worker 只回传首个错误, 避免在非测试主 goroutine 中调用 Fatal 类断言.
func TestConcurrentNewAndRelease(t *testing.T) {
	const (
		workers = 16
		rounds  = 20
		delay   = time.Millisecond
	)

	var wg sync.WaitGroup
	errors := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < rounds; round++ {
				start := time.Now()
				timer := New(delay)
				select {
				case fired := <-timer.C:
					Release(timer)
					if fired.Before(start.Add(delay)) {
						errors <- fmt.Errorf("worker %d round %d received stale timer event", worker, round)
						return
					}
				case <-time.After(timerTestTimeout):
					Release(timer)
					errors <- fmt.Errorf("worker %d round %d timed out", worker, round)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		assert.Nil(t, err)
	}
}
