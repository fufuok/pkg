package kit

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCounterInc(t *testing.T) {
	c := NewCounter()
	for i := range 100 {
		if v := c.Value(); v != int64(i) {
			t.Fatalf("got %v, want %d", v, i)
		}
		c.Inc()
	}
}

func TestCounterDec(t *testing.T) {
	c := NewCounter()
	for i := range 100 {
		if v := c.Value(); v != int64(-i) {
			t.Fatalf("got %v, want %d", v, -i)
		}
		c.Dec()
	}
}

func TestCounterAdd(t *testing.T) {
	c := NewCounter()
	for i := range 100 {
		if v := c.Value(); v != int64(i*42) {
			t.Fatalf("got %v, want %d", v, i*42)
		}
		c.Add(42)
	}
}

func TestCounterLoad(t *testing.T) {
	c := NewCounter()
	c.Add(100)
	if v := c.Load(); v != int64(100) {
		t.Fatalf("Load got %v, want %d", v, 100)
	}

	c.Add(-50)
	if v := c.Load(); v != int64(50) {
		t.Fatalf("Load got %v, want %d", v, 50)
	}
}

func TestCounterStore(t *testing.T) {
	c := NewCounter()
	c.Add(100)

	// Store a new value
	c.Store(42)
	if v := c.Value(); v != int64(42) {
		t.Fatalf("Value got %v, want %d", v, 42)
	}

	// Store zero
	c.Store(0)
	if v := c.Value(); v != int64(0) {
		t.Fatalf("Value got %v, want %d", v, 0)
	}

	// Store negative value
	c.Store(-100)
	if v := c.Value(); v != int64(-100) {
		t.Fatalf("Value got %v, want %d", v, -100)
	}
}

func TestCounterNegative(t *testing.T) {
	c := NewCounter()
	c.Add(-100)
	if v := c.Value(); v != -100 {
		t.Fatalf("got %v, want %d", v, -100)
	}
}

func TestCounterReset(t *testing.T) {
	c := NewCounter()
	c.Add(42)
	if v := c.Value(); v != 42 {
		t.Fatalf("got %v, want %d", v, 42)
	}
	c.Reset()
	if v := c.Value(); v != 0 {
		t.Fatalf("got %v, want %d", v, 0)
	}
}

func TestUCounterInc(t *testing.T) {
	c := NewUCounter()
	for i := range 100 {
		if v := c.Value(); v != uint64(i) {
			t.Fatalf("got %v, want %d", v, i)
		}
		c.Inc()
	}
}

func TestUCounterAdd(t *testing.T) {
	c := NewUCounter()
	for i := range 100 {
		if v := c.Value(); v != uint64(i*42) {
			t.Fatalf("got %v, want %d", v, i*42)
		}
		c.Add(42)
	}
}

func TestUCounterLoad(t *testing.T) {
	c := NewUCounter()
	for i := range 100 {
		if v := c.Load(); v != uint64(i) {
			t.Fatalf("Load got %v, want %d", v, i)
		}
		c.Inc()
	}
}

func TestUCounterStore(t *testing.T) {
	c := NewUCounter()
	c.Add(42)
	if v := c.Value(); v != 42 {
		t.Fatalf("Value got %v, want %d", v, 42)
	}

	c.Store(100)
	if v := c.Value(); v != 100 {
		t.Fatalf("Value got %v, want %d", v, 100)
	}

	c.Store(0)
	if v := c.Value(); v != 0 {
		t.Fatalf("Value got %v, want %d", v, 0)
	}
}

func TestUCounterReset(t *testing.T) {
	c := NewUCounter()
	c.Add(42)
	if v := c.Value(); v != 42 {
		t.Fatalf("got %v, want %d", v, 42)
	}
	c.Reset()
	if v := c.Value(); v != 0 {
		t.Fatalf("got %v, want %d", v, 0)
	}
}

func parallelIncrementor(c *Counter, numIncs int, cdone chan bool) {
	for range numIncs {
		c.Inc()
	}
	cdone <- true
}

func parallelUIncrementor(c *UCounter, numIncs int, cdone chan bool) {
	for range numIncs {
		c.Inc()
	}
	cdone <- true
}

func doTestParallelIncrementors(t *testing.T, numModifiers, gomaxprocs int) {
	runtime.GOMAXPROCS(gomaxprocs)
	c := NewCounter()
	cdone := make(chan bool)
	numIncs := 10_000
	for range numModifiers {
		go parallelIncrementor(c, numIncs, cdone)
	}
	// Wait for the goroutines to finish.
	for range numModifiers {
		<-cdone
	}
	expected := int64(numModifiers * numIncs)
	if v := c.Value(); v != expected {
		t.Fatalf("got %d, want %d", v, expected)
	}
}

func doTestParallelUIncrementors(t *testing.T, numModifiers, gomaxprocs int) {
	runtime.GOMAXPROCS(gomaxprocs)
	c := NewUCounter()
	cdone := make(chan bool)
	numIncs := 10_000
	for range numModifiers {
		go parallelUIncrementor(c, numIncs, cdone)
	}
	// Wait for the goroutines to finish.
	for range numModifiers {
		<-cdone
	}
	expected := uint64(numModifiers * numIncs)
	if v := c.Value(); v != expected {
		t.Fatalf("got %d, want %d", v, expected)
	}
}

func TestCounterParallelIncrementors(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(-1))
	doTestParallelIncrementors(t, 4, 2)
	doTestParallelIncrementors(t, 16, 4)
	doTestParallelIncrementors(t, 64, 8)
}

func TestUCounterParallelIncrementors(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(-1))
	doTestParallelUIncrementors(t, 4, 2)
	doTestParallelUIncrementors(t, 16, 4)
	doTestParallelUIncrementors(t, 64, 8)
}

func benchmarkCounter(b *testing.B, writeRatio int) {
	c := NewCounter()
	runParallel(b, func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			foo++
			if writeRatio > 0 && foo%writeRatio == 0 {
				c.Value()
			} else {
				c.Inc()
			}
		}
		_ = foo
	})
}

func BenchmarkCounterInt64(b *testing.B) {
	benchmarkCounter(b, 10000)
}

func benchmarkUCounter(b *testing.B, writeRatio int) {
	c := NewUCounter()
	runParallel(b, func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			foo++
			if writeRatio > 0 && foo%writeRatio == 0 {
				c.Value()
			} else {
				c.Inc()
			}
		}
		_ = foo
	})
}

func BenchmarkCounterUint64(b *testing.B) {
	benchmarkUCounter(b, 10000)
}

func benchmarkAtomicInt64(b *testing.B, writeRatio int) {
	var c atomic.Int64
	runParallel(b, func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			foo++
			if writeRatio > 0 && foo%writeRatio == 0 {
				_ = c.Load()
			} else {
				c.Add(1)
			}
		}
		_ = foo
	})
}

func BenchmarkCounterAtomicInt64(b *testing.B) {
	benchmarkAtomicInt64(b, 10000)
}

func benchmarkAtomicUint64(b *testing.B, writeRatio int) {
	var c atomic.Uint64
	runParallel(b, func(pb *testing.PB) {
		foo := 0
		for pb.Next() {
			foo++
			if writeRatio > 0 && foo%writeRatio == 0 {
				_ = c.Load()
			} else {
				c.Add(1)
			}
		}
		_ = foo
	})
}

func BenchmarkCounterAtomicUint64(b *testing.B) {
	benchmarkAtomicUint64(b, 10000)
}

func runParallel(b *testing.B, benchFn func(pb *testing.PB)) {
	b.ResetTimer()
	start := time.Now()
	b.RunParallel(benchFn)
	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

// 指定goroutine数量的并发基准测试
func runWithGoroutines(b *testing.B, numGoroutines int, benchFn func()) {
	b.ResetTimer()
	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	opsPerGoroutine := b.N / numGoroutines

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				benchFn()
			}
		}()
	}

	wg.Wait()

	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

func benchmarkCounterWithGoroutines(b *testing.B, numGoroutines int, c *Counter) {
	runWithGoroutines(b, numGoroutines, func() {
		c.Inc()
	})
}

func benchmarkUCounterWithGoroutines(b *testing.B, numGoroutines int, c *UCounter) {
	runWithGoroutines(b, numGoroutines, func() {
		c.Inc()
	})
}

func benchmarkAtomicInt64WithGoroutines(b *testing.B, numGoroutines int, c *atomic.Int64) {
	runWithGoroutines(b, numGoroutines, func() {
		c.Add(1)
	})
}

func benchmarkAtomicUint64WithGoroutines(b *testing.B, numGoroutines int, c *atomic.Uint64) {
	runWithGoroutines(b, numGoroutines, func() {
		c.Add(1)
	})
}

func BenchmarkCounterInt64G2(b *testing.B) {
	c := NewCounter()
	benchmarkCounterWithGoroutines(b, 2, c)
}

func BenchmarkCounterInt64G4(b *testing.B) {
	c := NewCounter()
	benchmarkCounterWithGoroutines(b, 4, c)
}

func BenchmarkCounterInt64G8(b *testing.B) {
	c := NewCounter()
	benchmarkCounterWithGoroutines(b, 8, c)
}

func BenchmarkCounterInt64G16(b *testing.B) {
	c := NewCounter()
	benchmarkCounterWithGoroutines(b, 16, c)
}

func BenchmarkCounterUint64G2(b *testing.B) {
	c := NewUCounter()
	benchmarkUCounterWithGoroutines(b, 2, c)
}

func BenchmarkCounterUint64G4(b *testing.B) {
	c := NewUCounter()
	benchmarkUCounterWithGoroutines(b, 4, c)
}

func BenchmarkCounterUint64G8(b *testing.B) {
	c := NewUCounter()
	benchmarkUCounterWithGoroutines(b, 8, c)
}

func BenchmarkCounterUint64G16(b *testing.B) {
	c := NewUCounter()
	benchmarkUCounterWithGoroutines(b, 16, c)
}

func BenchmarkCounterAtomicInt64G2(b *testing.B) {
	var c atomic.Int64
	benchmarkAtomicInt64WithGoroutines(b, 2, &c)
}

func BenchmarkCounterAtomicInt64G4(b *testing.B) {
	var c atomic.Int64
	benchmarkAtomicInt64WithGoroutines(b, 4, &c)
}

func BenchmarkCounterAtomicInt64G8(b *testing.B) {
	var c atomic.Int64
	benchmarkAtomicInt64WithGoroutines(b, 8, &c)
}

func BenchmarkCounterAtomicInt64G16(b *testing.B) {
	var c atomic.Int64
	benchmarkAtomicInt64WithGoroutines(b, 16, &c)
}

func BenchmarkCounterAtomicUint64G2(b *testing.B) {
	var c atomic.Uint64
	benchmarkAtomicUint64WithGoroutines(b, 2, &c)
}

func BenchmarkCounterAtomicUint64G4(b *testing.B) {
	var c atomic.Uint64
	benchmarkAtomicUint64WithGoroutines(b, 4, &c)
}

func BenchmarkCounterAtomicUint64G8(b *testing.B) {
	var c atomic.Uint64
	benchmarkAtomicUint64WithGoroutines(b, 8, &c)
}

func BenchmarkCounterAtomicUint64G16(b *testing.B) {
	var c atomic.Uint64
	benchmarkAtomicUint64WithGoroutines(b, 16, &c)
}

// 非并发基准测试
func benchmarkCounterSequential(b *testing.B, c *Counter) {
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

func benchmarkUCounterSequential(b *testing.B, c *UCounter) {
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

func benchmarkAtomicInt64Sequential(b *testing.B, c *atomic.Int64) {
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		c.Add(1)
	}
	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

func benchmarkAtomicUint64Sequential(b *testing.B, c *atomic.Uint64) {
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		c.Add(1)
	}
	opsPerSec := float64(b.N) / time.Since(start).Seconds()
	b.ReportMetric(opsPerSec, "ops/s")
}

func BenchmarkCounterInt64Sequential(b *testing.B) {
	c := NewCounter()
	benchmarkCounterSequential(b, c)
}

func BenchmarkCounterUint64Sequential(b *testing.B) {
	c := NewUCounter()
	benchmarkUCounterSequential(b, c)
}

func BenchmarkCounterAtomicInt64Sequential(b *testing.B) {
	var c atomic.Int64
	benchmarkAtomicInt64Sequential(b, &c)
}

func BenchmarkCounterAtomicUint64Sequential(b *testing.B) {
	var c atomic.Uint64
	benchmarkAtomicUint64Sequential(b, &c)
}

// # go test -run=^$ -benchmem -benchtime=1s -bench=BenchmarkCounter
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/kit
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// BenchmarkCounterInt64-16                        630565081                1.615 ns/op     619224471 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64-16                       735384720                1.666 ns/op     600165995 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64-16                  86836548                12.93 ns/op       77324279 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64-16                 94604388                12.86 ns/op       77778155 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterInt64G2-16                      173824995                7.165 ns/op     139557888 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterInt64G4-16                      305409253                3.854 ns/op     259459160 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterInt64G8-16                      521693586                2.187 ns/op     457312072 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterInt64G16-16                     594823736                1.749 ns/op     571810027 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64G2-16                     150947774                7.183 ns/op     139227004 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64G4-16                     297999195                3.948 ns/op     253295293 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64G8-16                     476478931                2.386 ns/op     419102476 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64G16-16                    630175339                1.767 ns/op     565959835 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64G2-16                124835595                9.941 ns/op     100592247 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64G4-16                100000000               11.04 ns/op       90580730 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64G8-16                100000000               11.77 ns/op       84956907 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64G16-16               99505548                12.97 ns/op       77083729 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64G2-16               122733769                9.738 ns/op     102695330 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64G4-16               100000000               11.45 ns/op       87331787 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64G8-16               100000000               11.98 ns/op       83483928 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64G16-16              98744008                12.85 ns/op       77814553 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterInt64Sequential-16              89113825                11.75 ns/op       85121346 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64Sequential-16             97037210                12.19 ns/op       82062549 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64Sequential-16        656915202                1.728 ns/op     578866048 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64Sequential-16       713845836                1.736 ns/op     575928325 ops/s               0 B/op          0 allocs/op
