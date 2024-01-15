package master

import (
	"context"

	"github.com/fufuok/utils/ntp"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	ntpCtx     context.Context
	ntpCancel  context.CancelFunc
	ntpRunning bool
)

// 根据配置开启或关闭默认的时间同步服务
func startTimeSync() error {
	if config.Config().SYSConf.DisableTimeSync {
		_ = stopTimeSync()
		return nil
	}
	if ntpRunning {
		return nil
	}
	go ntpdate()
	return nil
}

func runtimeTimeSync() error {
	return startTimeSync()
}

func stopTimeSync() error {
	if ntpRunning && ntpCancel != nil {
		ntpCancel()
	}
	ntpRunning = false
	return nil
}

// 时间同步服务
func ntpdate() {
	// 首次同步
	ntpRunning = true
	logger.Warn().Msg("ntpdate is working")
	ntpCtx, ntpCancel = context.WithTimeout(context.Background(), common.ClockOffsetFirstInterval*4)
	dur := <-ntp.ClockOffsetChan(ntpCtx, common.ClockOffsetFirstInterval)
	logger.Warn().Str("clock_offset", dur.String()).Msg("first ntpdate")
	ntpCancel()
	common.SetClockOffset(dur)

	// 定时同步
	ntpCtx, ntpCancel = context.WithCancel(context.Background())
	ch := ntp.ClockOffsetChan(ntpCtx, 0)
	for dur = range ch {
		common.SetClockOffset(dur)
	}
	logger.Warn().Str("clock_offet", common.GetClockOffset().String()).Msg("ntpdate exited")
}
