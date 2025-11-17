package kit

import (
	"math/rand"
	"testing"
)

func TestNextPowOf2(t *testing.T) {
	if NextPowOf2(0) != 1 {
		t.Error("NextPowOf2 failed")
	}
	if NextPowOf2(1) != 1 {
		t.Error("NextPowOf2 failed")
	}
	if NextPowOf2(2) != 2 {
		t.Error("NextPowOf2 failed")
	}
	if NextPowOf2(3) != 4 {
		t.Error("NextPowOf2 failed")
	}
}

// This test is here to catch potential problems
// with cheaprand-related changes.
func TestCheaprand(t *testing.T) {
	count := 100
	set := make(map[uint32]struct{}, count)

	for range count {
		num := Cheaprand()
		set[num] = struct{}{}
	}

	if len(set) != count {
		t.Error("duplicated rand num")
	}
}

func BenchmarkRandCheaprand(b *testing.B) {
	for b.Loop() {
		_ = Cheaprand()
	}
	// <1.4 ns/op on x86-64
}

func BenchmarkRand(b *testing.B) {
	for b.Loop() {
		_ = rand.Uint32()
	}
	// about 5 ns/op on x86-64
}

// # go test -run=^$ -benchmem -benchtime=1s -bench=BenchmarkRand
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/kit
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// BenchmarkRandCheaprand-16       495433686                2.457 ns/op           0 B/op          0 allocs/op
// BenchmarkRand-16                161147025                7.427 ns/op           0 B/op          0 allocs/op
