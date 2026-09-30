package utils

import (
	"testing"

	"github.com/fufuok/pkg/assert"
)

// TestFastRandPublicFunctions 保证两个公开随机入口被真实链接并可调用.
// 不断言具体随机值; 旧 FastRandu 只有被调用时才暴露已移除 runtime 符号的链接错误.
func TestFastRandPublicFunctions(t *testing.T) {
	t.Parallel()
	t.Logf("random values: uint64=%d, uint=%d", FastRand64(), FastRandu())
}

func TestNewRand(t *testing.T) {
	rd := NewRand(1)
	assert.Equal(t, int64(5577006791947779410), rd.Int63())

	rd = NewRand()
	for i := 1; i < 1000; i++ {
		assert.Equal(t, true, rd.Intn(i) < i)
		assert.Equal(t, true, rd.Int63n(int64(i)) < int64(i))
		assert.Equal(t, true, Rand.Intn(i) < i)
		assert.Equal(t, true, Rand.Int63n(int64(i)) < int64(i))
	}
}
