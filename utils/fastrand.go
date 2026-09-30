package utils

import (
	"math/rand"
	randv2 "math/rand/v2"
	"sync"
	"time"
)

// FastRand64 返回覆盖 uint64 全部取值范围的伪随机数, 可并发调用.
// 使用标准库的进程随机源, 不提供可重现序列, 不适用于密钥或安全令牌.
func FastRand64() uint64 {
	return randv2.Uint64()
}

// FastRandu 返回覆盖 uint 全部取值范围的伪随机数, 位宽随目标平台变化, 可并发调用.
// 使用标准库公开入口以避免依赖已移除的 runtime.fastrandu 符号, 不适用于密码学用途.
func FastRandu() uint {
	return randv2.Uint()
}

// Implement Source and Source64 interfaces
type rngSource struct {
	p sync.Pool
}

func (r *rngSource) Int63() (n int64) {
	src := r.p.Get()
	n = src.(rand.Source).Int63()
	r.p.Put(src)
	return
}

// Seed specify seed when using NewRand()
func (r *rngSource) Seed(_ int64) {}

func (r *rngSource) Uint64() (n uint64) {
	src := r.p.Get()
	n = src.(rand.Source64).Uint64()
	r.p.Put(src)
	return
}

// NewRand goroutine-safe rand.Rand, optional seed value
func NewRand(seed ...int64) *rand.Rand {
	n := time.Now().UnixNano()
	if len(seed) > 0 {
		n = seed[0]
	}
	src := &rngSource{
		p: sync.Pool{
			New: func() any {
				return rand.NewSource(n)
			},
		},
	}
	return rand.New(src)
}
