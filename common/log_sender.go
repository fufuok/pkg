package common

import (
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool/buffer"
	"github.com/fufuok/chanx"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/config"
)

// LogChan 日志缓存队列
var LogChan *chanx.UnboundedChan[[]byte]

func initLogSender() {
	LogChan = NewChanx[[]byte](config.ChanxInitCap)
	go logSender()
}

// 定时推送日志到日志收集接口
//
//nolint:cyclop
func logSender() {
	num := 0
	bb := buffer.Get()
	cfg := config.Config().LogConf
	lastLoadTime := time.Now()
	interval := cfg.PostIntervalDuration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case t := <-ticker.C:
			// 定期重新加载配置
			if t.Sub(lastLoadTime) > config.DefaultLoadConfigInterval {
				lastLoadTime = t
				cfg = config.Config().LogConf
				if interval != cfg.PostIntervalDuration {
					interval = cfg.PostIntervalDuration
					ticker.Reset(interval)
				}
			}
			if bb.Len() > 0 {
				postLog(bb)
				bb = buffer.Get()
				num = 0
			}
		case bs, ok := <-LogChan.Out:
			if !ok {
				if bb.Len() > 0 {
					postLog(bb)
				}
				Log().Warn().Msg("Log sender exited")
				return
			}

			// ,{"json":"..."}
			_ = bb.WriteByte(',')
			_, _ = bb.Write(bs)

			// 按条数或内容大小分批发送
			num++
			if num%cfg.PostBatchNum == 0 || bb.Len() > cfg.PostBatchBytes {
				postLog(bb)
				bb = buffer.Get()
				num = 0
			}
		}
	}
}

// postLog 转交批次缓冲区给发送任务, 固定提交时的接口地址, 避免异步读取变化中的配置.
func postLog(bb *buffer.Buffer) {
	api := config.Config().LogConf.PostAPI
	if api == "" {
		bb.Put()
		return
	}

	// [{"json":"..."},{"json":"..."}]
	bb.B[0] = '['
	_ = bb.WriteByte(']')

	// 推送日志数据到接口 POST JSON
	_ = ants.Submit(func() {
		defer bb.Put()
		if _, err := req.SetBodyJsonBytes(bb.B).Post(api); err != nil {
			LogSampled().Warn().Err(err).Str("api", api).Msg("Posting log")
		}
	})
}

// PostLog 异步推送日志, 使用调用时已发布的接口地址, 不依赖 sender 的周期配置刷新.
func PostLog(bs []byte) {
	api := config.Config().LogConf.PostAPI
	if api == "" {
		return
	}

	// 推送日志数据到接口 POST JSON
	_ = ants.Submit(func() {
		if _, err := req.SetBodyJsonBytes(bs).Post(api); err != nil {
			LogSampled().Warn().Err(err).Str("api", api).Msg("Posting log")
		}
	})
}
