package queue

import (
	"context"
	"sync"
	"testing"

	"github.com/fufuok/chanx"
)

const (
	numProducers = 8
	numItems     = 100000
)

func Benchmark_MPSC_UMPSCQueue(b *testing.B) {
	for n := 0; n < b.N; n++ {
		q := New[int]()
		var wg sync.WaitGroup
		wg.Add(numProducers)

		// 多生产者
		for i := 0; i < numProducers; i++ {
			go func() {
				for j := 0; j < numItems; j++ {
					q.Enqueue(j)
				}
				wg.Done()
			}()
		}

		// 单消费者
		count := numProducers * numItems
		for i := 0; i < count; i++ {
			_ = q.Dequeue()
		}
		wg.Wait()
	}
}

func Benchmark_MPSC_Chanx(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ch := chanx.NewUnboundedChan[int](context.Background(), 1024)
		var wg sync.WaitGroup
		wg.Add(numProducers)

		// 多生产者
		for i := 0; i < numProducers; i++ {
			go func() {
				for j := 0; j < numItems; j++ {
					ch.In <- j
				}
				wg.Done()
			}()
		}

		// 单消费者
		count := numProducers * numItems
		for i := 0; i < count; i++ {
			_ = <-ch.Out
		}
		wg.Wait()
	}
}

func Benchmark_MPSC_Channel(b *testing.B) {
	for n := 0; n < b.N; n++ {
		ch := make(chan int, 1024)
		var wg sync.WaitGroup
		wg.Add(numProducers)

		// 多生产者
		for i := 0; i < numProducers; i++ {
			go func() {
				for j := 0; j < numItems; j++ {
					ch <- j
				}
				wg.Done()
			}()
		}

		// 单消费者
		count := numProducers * numItems
		for i := 0; i < count; i++ {
			_ = <-ch
		}
		wg.Wait()
	}
}

// # go test -bench=Benchmark_MPSC -benchmem -benchtime=1s
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/queue
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// Benchmark_MPSC_UMPSCQueue-16                  86          15580719 ns/op         3119613 B/op        259 allocs/op
// Benchmark_MPSC_Chanx-16                        7         145428886 ns/op          481664 B/op         30 allocs/op
// Benchmark_MPSC_Channel-16                     33          34029261 ns/op           10321 B/op         11 allocs/op
// PASS
// ok      github.com/fufuok/pkg/queue     3.906s

// # go test -bench=Benchmark_MPSC -benchmem -benchtime=5s
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/queue
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// Benchmark_MPSC_UMPSCQueue-16                 381          15844017 ns/op         3792980 B/op        262 allocs/op
// Benchmark_MPSC_Chanx-16                       40         147074348 ns/op          534104 B/op         29 allocs/op
// Benchmark_MPSC_Channel-16                    172          36189968 ns/op            9757 B/op         10 allocs/op
// PASS
// ok      github.com/fufuok/pkg/queue     23.603s
