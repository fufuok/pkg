package tproxy

import (
	"testing"

	"github.com/fufuok/utils/assert"
	"github.com/fufuok/utils/xhash"
)

func testXToken() string {
	xip := "118.118.8.8"
	xtime := "2024-04-10T14:11:00+08:00"
	tokenSalt := "test-salt"
	xtoken := xhash.HashString(xip, xtime, tokenSalt)
	return xtoken
}

func TestSetClientIP(t *testing.T) {
	xtoken := testXToken()
	assert.Equal(t, "13132673241273045767", xtoken)
}

func BenchmarkSetClientIP(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = testXToken()
	}
}

// go test -run=nil -benchmem -bench=BenchmarkSetClientIP
// goos: linux
// goarch: amd64
// pkg: github.com/fufuok/pkg/web/fiber/tproxy
// cpu: AMD Ryzen 7 5700G with Radeon Graphics
// BenchmarkSetClientIP-16         10461621               115.2 ns/op            72 B/op          2 allocs/op
