package common

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/fufuok/pkg/config"
)

var (
	// StartTime 系统启动时间
	StartTime = time.Now()

	// ClockOffsetLimit 允许的时间偏差, 1 秒内偏差不更新 clockOffset
	ClockOffsetLimit = float64(1 * time.Second)

	// ClockOffsetAdjust 当时间偏差需要修正时, 默认拨快 10ms
	ClockOffsetAdjust = int64(10 * time.Millisecond)

	// ClockOffsetMinInterval 同步时间的单次执行间隔
	ClockOffsetMinInterval = 20 * time.Second
	ClockOffsetInterval    = 2 * time.Hour

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

// SetClockOffset 设置时钟偏移值
func SetClockOffset(dur time.Duration) {
	offset := int64(dur)
	if math.Abs(float64(offset-clockOffset.Load())) >= ClockOffsetLimit {
		clockOffset.Store(offset + ClockOffsetAdjust)
		Log.Warn().Str("clock_offset", GetClockOffset().String()).Msg("ntpdate")
	}
}
