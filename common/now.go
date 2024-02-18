package common

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils"

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
	currentTime atomic.Pointer[CurrentTime]
)

// CurrentTime 当前时间, 预格式化的字符串形式 (秒级)
type CurrentTime struct {
	// 强制 0 时区的 8 时区时间 (仅特定场景展示使用)
	Str3339Z string
	// 带时区的正确时间值
	Str3339 string
	// 年月日, 注: 带前缀下划线: _220720
	StrYmd string
	// 时间戳
	Unix int64
	// 当前时间
	Time time.Time
}

func Now() *CurrentTime {
	return currentTime.Load()
}

func SetupNow() {
	initNow(StartTime)
}

func initNow(t time.Time) {
	if t.Equal(StartTime) {
		StartTime = StartTime.In(config.DefaultTimeLocation)
		t = StartTime
	}
	currentTime.Store(&CurrentTime{
		t.Format("2006-01-02T15:04:05Z"),
		t.Format(time.RFC3339),
		t.Format("_060102"),
		t.Unix(),
		t,
	})
}

// 周期性更新全局时间字段
func syncNow() {
	for {
		t := GTimeNow()
		t = utils.WaitNextSecondWithTime(t)
		initNow(t)
	}
}

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
