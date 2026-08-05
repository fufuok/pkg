//go:build !windows

package utils

import (
	"sync"
	"testing"
	"time"
)

// TestWaitNextSecond 验证并发等待会对齐到下一整秒且不会提前返回.
// 调度器可能在目标时间后任意时刻恢复 goroutine, 测试不得限制恢复延迟.
func TestWaitNextSecond(t *testing.T) {
	const (
		rounds          = 3
		workersPerRound = 3
	)
	type waitResult struct {
		wantTarget time.Time
		gotTarget  time.Time
		finished   time.Time
	}
	results := make(chan waitResult, rounds*(workersPerRound+1))
	for i := 0; i < rounds; i++ {
		var wg sync.WaitGroup
		for j := 0; j < workersPerRound; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				wantTarget := BeginOfSecond(start.Add(time.Second))
				gotTarget := WaitNextSecondWithTime(start)
				results <- waitResult{wantTarget: wantTarget, gotTarget: gotTarget, finished: time.Now()}
			}()
		}

		start := time.Now()
		wantTarget := BeginOfSecond(start.Add(time.Second))
		WaitNextSecond(start)
		results <- waitResult{wantTarget: wantTarget, gotTarget: wantTarget, finished: time.Now()}
		wg.Wait()
	}

	close(results)
	for result := range results {
		if !result.gotTarget.Equal(result.wantTarget) {
			t.Errorf("target = %s, want %s", result.gotTarget.Format(time.RFC3339Nano), result.wantTarget.Format(time.RFC3339Nano))
		}
		if result.finished.Before(result.wantTarget) {
			t.Errorf("finished at %s before target %s", result.finished.Format(time.RFC3339Nano), result.wantTarget.Format(time.RFC3339Nano))
		}
	}
}
