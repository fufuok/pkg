package common

import (
	"context"

	"github.com/fufuok/ants"
	"github.com/fufuok/chanx"

	"github.com/fufuok/pkg/config"
)

// MaxGoPool 最大协程数, 限定并发处理能力上限
var MaxGoPool = 200_000

func initPool() {
	size := config.DefaultGOMAXPROCS
	pool, _ := ants.NewMultiPool(
		size,
		MaxGoPool/size,
		ants.RoundRobin,
		ants.WithNonblocking(true),
		ants.WithLogger(NewAppLogger()),
		ants.WithPanicHandler(func(r any) {
			LogSampled().Error().Msgf("Recovery worker: %s", r)
		}),
	)
	ants.SetDefaultPool(pool)
}

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
