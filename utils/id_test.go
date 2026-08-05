package utils_test

import (
	"testing"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/utils"
)

func TestID(t *testing.T) {
	a := utils.ID()
	b := utils.ID()
	assert.Equal(t, b, a+1)
}

func TestGoroutineID(t *testing.T) {
	gid, err := utils.GoroutineID()
	assert.Equal(t, nil, err)
	t.Log(gid)
}

func BenchmarkGoroutineID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := utils.GoroutineID()
		if err != nil {
			b.Error(err)
		}
	}
}
