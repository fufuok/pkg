package master

import (
	"context"
	"time"

	"github.com/fufuok/ntp"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	ntpCtx    context.Context
	ntpCancel context.CancelFunc
	ntpName   string
)

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
	ntpCtx, ntpCancel = context.WithTimeout(context.Background(), common.ClockOffsetMinInterval*4)
	name, ch := getClockOffsetChan(ntpCtx, common.ClockOffsetMinInterval)
	if ch == nil {
		return
	}
	dur := <-ch
	logger.Warn().Str("clock_offset", dur.String()).Str("name", name).Msg("first ntpdate")
	ntpCancel()
	common.SetClockOffset(dur)

	// 定时同步
	ntpCtx, ntpCancel = context.WithCancel(context.Background())
	name, ch = getClockOffsetChan(ntpCtx, common.ClockOffsetInterval)
	if ch == nil {
		return
	}
	logger.Warn().Str("name", name).Msg("ntpdate is working")
	for dur = range ch {
		common.SetClockOffset(dur)
	}
	logger.Warn().Str("clock_offet", common.GetClockOffset().String()).Str("type", name).Msg("ntpdate exited")
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
