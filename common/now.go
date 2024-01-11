package common

import (
	"sync/atomic"
	"time"

	"github.com/fufuok/utils"

	"github.com/fufuok/pkg/config"
)

var (
	// StartTime 系统启动时间
	StartTime = time.Now()

	currentTime atomic.Pointer[CurrentTime]
)

// CurrentTime 当前时间, 预格式化的字符串形式 (秒级)
type CurrentTime struct {
	// 带时区的时间值
	Str3339 string
	// 时间戳
	Unix int64
	// 当前时间
	Time time.Time
}

func Now() *CurrentTime {
	return currentTime.Load()
}

func initNow(t time.Time) {
	if t.Equal(StartTime) {
		StartTime = StartTime.In(config.DefaultTimeLocation)
		t = StartTime
	}
	currentTime.Store(&CurrentTime{
		t.Format(time.RFC3339),
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
