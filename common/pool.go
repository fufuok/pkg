package common

import (
	"context"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/chanx"

	"github.com/fufuok/pkg/config"
)

const (
	// expiryDuration is the interval time to clean up those expired workers.
	expiryDuration = 10 * time.Second
)

func initPool() {
	ants.SetDefaultAntsPool(
		ants.DefaultAntsPoolSize,
		ants.WithExpiryDuration(expiryDuration),
		ants.WithNonblocking(true),
		ants.WithLogger(NewAppLogger()),
	)
}

func poolRelease() {
	ants.Release()
}

// NewChanxOf 初始化无限缓冲信道
func NewChanxOf[T any](maxBufferSize ...int) *chanx.UnboundedChanOf[T] {
	return NewChanxWithContextOf[T](context.Background(), maxBufferSize...)
}

func NewChanxWithContextOf[T any](ctx context.Context, maxBufferSize ...int) *chanx.UnboundedChanOf[T] {
	m := config.ChanxMaxBufCap
	if len(maxBufferSize) > 0 {
		m = maxBufferSize[0]
	}
	return chanx.NewUnboundedChanOf[T](ctx, config.ChanxInitCap, m)
}
