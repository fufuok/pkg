package queue

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// TestQueue_EnqueueCount 验证读数不清零, 且累计入队量不受消费影响.
func TestQueue_EnqueueCount(t *testing.T) {
	q := New[int]()
	if count := q.EnqueueCount(); count != 0 {
		t.Fatalf("Expected initial count 0, got %d", count)
	}
	q.Enqueue(42)
	for range 3 {
		if count := q.EnqueueCount(); count != 1 {
			t.Fatalf("Expected count 1 without reset, got %d", count)
		}
	}
	if value := q.Dequeue(); value != 42 {
		t.Fatalf("Expected queued value 42, got %d", value)
	}
	if count := q.EnqueueCount(); count != 1 {
		t.Fatalf("Expected lifetime count 1 after dequeue, got %d", count)
	}
	if length := q.Len(); length != 0 {
		t.Fatalf("Expected empty queue after dequeue, got length %d", length)
	}
}

// TestQueue_EnqueueCountPreservesRateWindow 验证独立读取累计不会消费速率窗口.
// 使用虚拟时间跨过最小采样间隔, 避免错误地调用 RateState 也能通过断言.
func TestQueue_EnqueueCountPreservesRateWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := New[int]()
		q.Enqueue(1)
		q.RateState()
		time.Sleep(2 * time.Second)
		q.Enqueue(2)
		for range 3 {
			if count := q.EnqueueCount(); count != 2 {
				t.Fatalf("Expected count 2 without rate sampling, got %d", count)
			}
		}
		time.Sleep(2 * time.Second)
		q.Enqueue(3)
		rate, count, lastCount := q.RateState()
		if rate != 0.5 || count != 3 || lastCount != 1 {
			t.Fatalf("Expected untouched rate window (0.5, 3, 1), got (%v, %d, %d)", rate, count, lastCount)
		}
	})
}

// TestQueue_EnqueueCountConcurrent 验证多生产者, 单消费者和独立累计读取可并行使用.
// 运行中仅检查累计边界, 全部生产者退出后才要求精确总数.
func TestQueue_EnqueueCountConcurrent(t *testing.T) {
	const producers = 4
	const perProducer = 1000
	const total = producers * perProducer
	q := New[int]()
	var writers, readers, consumer sync.WaitGroup
	done := make(chan struct{})
	for range 2 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					if count := q.EnqueueCount(); count > total {
						t.Errorf("Expected count at most %d, got %d", total, count)
						return
					}
				}
			}
		})
	}
	consumer.Go(func() {
		for range total {
			q.Dequeue()
		}
	})
	for producer := range producers {
		writers.Go(func() {
			for item := range perProducer {
				q.Enqueue(producer*perProducer + item)
			}
		})
	}
	writers.Wait()
	close(done)
	readers.Wait()
	consumer.Wait()
	if count := q.EnqueueCount(); count != total {
		t.Fatalf("Expected final count %d, got %d", total, count)
	}
}
