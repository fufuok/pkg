package master

import (
	"context"
	"sync"
	"time"

	"github.com/fufuok/pkg/internal/ntp"
	"github.com/fufuok/pkg/pools/timerpool"
	"github.com/fufuok/pkg/utils"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	ntpMu            sync.Mutex
	ntpName          string
	ntpCancel        context.CancelFunc
	ntpDone          chan struct{}
	ntpFirstDoneChan = make(chan struct{})

	// clockOffsetChanOf 构造时间同步通道.
	// 默认走 getClockOffsetChan; 包内测试可替换以避免公网 NTP, 不支持并行改写.
	clockOffsetChanOf = func(ctx context.Context, dur time.Duration) (string, chan time.Duration) {
		return getClockOffsetChan(ctx, dur)
	}
)

// WaitUntilNtpdate 等待, 直到第一次时间同步成功
func WaitUntilNtpdate(timeout time.Duration) bool {
	timer := timerpool.New(timeout)
	defer timerpool.Release(timer)
	select {
	case <-timer.C:
		return false
	case <-ntpFirstDone():
		return true
	}
}

// ntpFirstDone 返回当前首次同步完成通道, 热更新重启后会换成新 channel.
func ntpFirstDone() <-chan struct{} {
	ntpMu.Lock()
	defer ntpMu.Unlock()
	return ntpFirstDoneChan
}

// 根据配置开启或关闭默认的时间同步服务
func startTimeSync() error {
	newType := config.Config().SYSConf.TimeSyncType
	if newType == "" {
		_ = stopTimeSync()
		return nil
	}
	ntpMu.Lock()
	same := newType == ntpName && ntpDone != nil
	ntpMu.Unlock()
	if same {
		return nil
	}
	if err := stopTimeSync(); err != nil {
		return err
	}

	// 先换新通道再启动循环, 避免 WaitUntilNtpdate 卡在上一轮已废弃的 done channel.
	// 父 ctx 覆盖首次同步和周期同步, stop 只取消这一处, 不会漏掉后半段循环.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	firstDone := make(chan struct{})
	ntpMu.Lock()
	ntpCancel = cancel
	ntpDone = done
	ntpFirstDoneChan = firstDone
	ntpName = newType
	ntpMu.Unlock()
	go ntpdate(ctx, done, firstDone)
	return nil
}

func runtimeTimeSync() error {
	return startTimeSync()
}

func stopTimeSync() error {
	// 只取消父 ctx, 不等待旧循环退出.
	// NTP/Redis 通道在 ticker 等待和发送时观察 ctx, 取消后应自行关闭; 这里仍不 Join, 避免查询 RTT 把 Stop 拉长.
	ntpMu.Lock()
	cancel := ntpCancel
	ntpCancel = nil
	ntpDone = nil
	ntpName = ""
	ntpMu.Unlock()
	if cancel != nil {
		cancel()
		common.SetClockOffset(0)
	}
	return nil
}

// 时间同步服务
func ntpdate(parent context.Context, done, firstDone chan struct{}) {
	if done == nil {
		return
	}
	defer close(done)

	// 首次同步受父 ctx 和短超时双重约束, 超时取消不影响后续周期同步.
	ctx, cancel := context.WithTimeout(parent, common.ClockOffsetMinInterval*4)
	name, ch := clockOffsetChanOf(ctx, common.ClockOffsetMinInterval) // 测试可替换, 避免首次同步打到公网 NTP.
	if ch == nil {
		cancel()
		return
	}
	dur, ok := <-ch
	cancel()
	if !ok {
		return
	}
	logger.Warn().Str("clock_offset", dur.String()).Str("name", name).Msg("Initial NTP sync completed")
	common.SetClockOffset(dur)
	if firstDone != nil {
		close(firstDone)
	}

	// 定时同步只跟父 ctx, stopTimeSync 取消后通道应关闭并退出.
	name, ch = clockOffsetChanOf(parent, common.ClockOffsetInterval)
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
	name := config.Config().SYSConf.TimeSyncType
	if name == "" {
		setNTPName("")
		return "", nil
	}
	name = utils.ToLower(name)
	setNTPName(name)
	if name == "redis" && common.RedisDBInited.Load() {
		return name, common.ClockOffsetChanRedis(ctx, dur, common.RedisDB)
	}
	return name, ntp.ClockOffsetChan(ctx, dur)
}

// setNTPName 记录当前时间同步类型, 供 start/stop 判断是否需要重启.
func setNTPName(name string) {
	ntpMu.Lock()
	ntpName = name
	ntpMu.Unlock()
}
