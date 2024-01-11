package common

import (
	"context"
	"math"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils/ntp"

	"github.com/fufuok/pkg/config"
)

const (
	// 允许的时间偏差, 1 秒内偏差不更新 clockOffset
	clockOffsetLimit = float64(time.Second)

	// 首次同步时间的执行间隔
	clockOffsetFirstInterval = 20 * time.Second
)

var (
	// 全局时间差
	clockOffset atomic.Int64
)

// GTimeNow 全局统一时间
func GTimeNow() time.Time {
	return time.Now().Add(GetClockOffset()).In(config.DefaultTimeLocation)
}

// GTimestamp 全局统一时间戳
func GTimestamp() int64 {
	return GTimeNow().Unix()
}

// GTimeNowString 全局统一时间并格式化
func GTimeNowString(layout string) string {
	if layout == "" {
		layout = time.RFC3339
	}
	return GTimeNow().Format(layout)
}

// GetClockOffset 获取时钟偏移值
func GetClockOffset() time.Duration {
	return time.Duration(clockOffset.Load())
}

// 时间同步服务
func ntpdate() {
	// 首次同步
	ctx, cancel := context.WithTimeout(context.Background(), clockOffsetFirstInterval*4)
	dur := <-ntp.ClockOffsetChan(ctx, clockOffsetFirstInterval)
	Log.Warn().Str("clock_offset", dur.String()).Msg("first ntpdate")
	cancel()
	setClockOffset(dur)

	// 定时同步
	ch := ntp.ClockOffsetChan(context.Background(), 0)
	for dur = range ch {
		setClockOffset(dur)
	}
	LogAlarm.Error().Msg("Exception: ntpdate worker exited")
}

func setClockOffset(dur time.Duration) {
	offset := int64(dur)
	if math.Abs(float64(offset-clockOffset.Load())) >= clockOffsetLimit {
		clockOffset.Store(offset)
		Log.Warn().Str("clock_offset", GetClockOffset().String()).Msg("ntpdate")
	}
}
