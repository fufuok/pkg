package xslices

import (
	"testing"

	"github.com/fufuok/pkg/assert"
)

func TestFilter(t *testing.T) {
	in := []int{7, -1, 0, 1, 2, 2}
	want := []int{7, 1, 2, 2}
	got := Filter(in, func(v int) bool {
		return v > 0
	})
	assert.Equal(t, want, got)
}
