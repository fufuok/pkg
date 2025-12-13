package common

import (
	"context"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/chanx"

	"github.com/fufuok/pkg/config"
)

// DefaultWorkerExpiry is the interval time to clean up those expired workers.
const DefaultWorkerExpiry = 10 * time.Second

func initPool() {
	ants.SetDefaultAntsPool(
		ants.DefaultAntsPoolSize,
		ants.WithExpiryDuration(DefaultWorkerExpiry),
		ants.WithNonblocking(true),
		ants.WithLogger(NewAppLogger()),
		ants.WithPanicHandler(func(r any) {
			LogSampled().Error().Msgf("Recovery worker: %s", r)
		}),
	)
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
