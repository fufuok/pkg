package kit

import (
	"runtime"
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

// # go test -run=^$ -benchmem -benchtime=1s -bench=BenchmarkCounter
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/kit
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// BenchmarkCounterInt64-16                613779258                1.634 ns/op     612022355 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterUint64-16               720991695                1.714 ns/op     583318613 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicInt64-16          87088257                13.03 ns/op       76719817 ops/s               0 B/op          0 allocs/op
// BenchmarkCounterAtomicUint64-16         94866549                12.84 ns/op       77904768 ops/s               0 B/op          0 allocs/op
