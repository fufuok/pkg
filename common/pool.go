package common

import (
	"context"

	"github.com/fufuok/ants"
	"github.com/fufuok/chanx"

	"github.com/fufuok/pkg/config"
)

// MaxGoPool 最大协程数, 限定并发处理能力上限
var MaxGoPool = 200_000

// newDefaultPool 构造生产和测试共用的默认协程池实现.
// 调用方负责决定安装方式和资源所有权, 本函数不修改 ants 全局默认池.
func newDefaultPool() (ants.Pooler, error) {
	size := config.DefaultGOMAXPROCS
	return ants.NewMultiPool(
		size,
		MaxGoPool/size,
		ants.RoundRobin,
		ants.WithNonblocking(true),
		ants.WithLogger(NewAppLogger()),
		ants.WithPanicHandler(func(r any) {
			LogSampled().Error().Msgf("Recovery worker: %s", r)
		}),
	)
}

// initPool 使用生产语义安装默认池, ants 会释放被替换的旧池.
func initPool() {
	pool, _ := newDefaultPool()
	ants.SetDefaultPool(pool)
}

// poolRelease 释放当前默认池, 用于完整生产生命周期停止.
func poolRelease() {
	ants.Release()
}

// NewChanx 初始化无限缓冲信道
func NewChanx[T any](maxBufferSize ...int) *chanx.UnboundedChan[T] {
	return NewChanxWithContext[T](context.Background(), maxBufferSize...)
}

func NewChanxWithContext[T any](ctx context.Context, maxBufferSize ...int) *chanx.UnboundedChan[T] {
	m := config.ChanxMaxBufCap
	if len(maxBufferSize) > 0 {
		m = maxBufferSize[0]
	}
	return chanx.NewUnboundedChan[T](ctx, config.ChanxInitCap, m)
}
