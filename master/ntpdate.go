package master

import (
	"context"
	"time"

	"github.com/fufuok/ntp"
	"github.com/fufuok/utils/pools/timerpool"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	ntpName          string
	ntpCancel        context.CancelFunc
	ntpFirstDoneChan = make(chan struct{})
)

// WaitUntilNtpdate 等待, 直到第一次时间同步成功
func WaitUntilNtpdate(timeout time.Duration) bool {
	timer := timerpool.New(timeout)
	defer timerpool.Release(timer)
	select {
	case <-timer.C:
		return false
	case <-ntpFirstDoneChan:
		return true
	}
}

// 根据配置开启或关闭默认的时间同步服务
func startTimeSync() error {
	newType := config.Config().SYSConf.TimeSyncType
	if newType == "" {
		_ = stopTimeSync()
		return nil
	}
	if newType == ntpName {
		return nil
	}
	if ntpName != "" {
		_ = stopTimeSync()
	}
	go ntpdate()
	return nil
}

func runtimeTimeSync() error {
	return startTimeSync()
}

func stopTimeSync() error {
	if ntpName != "" && ntpCancel != nil {
		ntpCancel()
		common.SetClockOffset(0)
	}
	ntpName = ""
	return nil
}

// 时间同步服务
func ntpdate() {
	// 首次同步
	var ctx context.Context
	ctx, ntpCancel = context.WithTimeout(context.Background(), common.ClockOffsetMinInterval*4)
	name, ch := getClockOffsetChan(ctx, common.ClockOffsetMinInterval)
	if ch == nil {
		return
	}
	dur := <-ch
	logger.Warn().Str("clock_offset", dur.String()).Str("name", name).Msg("Initial NTP sync completed")
	ntpCancel()
	common.SetClockOffset(dur)

	// 首次时间同步完成
	close(ntpFirstDoneChan)

	// 定时同步
	ctx, ntpCancel = context.WithCancel(context.Background())
	name, ch = getClockOffsetChan(ctx, common.ClockOffsetInterval)
	if ch == nil {
		return
	}
	logger.Warn().Str("name", name).Msg("NTP sync service started")
	for dur = range ch {
		common.SetClockOffset(dur)
	}
	logger.Warn().Str("clock_offet", common.GetClockOffset().String()).Str("type", name).Msg("NTP sync service stopped")
}

func getClockOffsetChan(ctx context.Context, dur time.Duration) (string, chan time.Duration) {
	ntpName = config.Config().SYSConf.TimeSyncType
	if ntpName == "" {
		return "", nil
	}
	if ntpName == "redis" && common.RedisDB != nil {
		return ntpName, common.ClockOffsetChanRedis(ctx, dur, common.RedisDB)
	}
	return ntpName, ntp.ClockOffsetChan(ctx, dur)
}
