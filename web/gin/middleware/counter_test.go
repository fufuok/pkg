package middleware

import (
	"sync"
	"testing"
)

// TestCounterStatsConcurrent 覆盖没有请求流量时多个状态查询共享采样器的场景.
func TestCounterStatsConcurrent(t *testing.T) {
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for range 1000 {
				CounterStats()
			}
		})
	}
	workers.Wait()
}
