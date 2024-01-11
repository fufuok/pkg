package common

import (
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool/buffer"
	"github.com/fufuok/chanx"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/config"
)

var (
	// LogChan 日志缓存队列
	LogChan *chanx.UnboundedChanOf[[]byte]
)

func initLogSender() {
	LogChan = NewChanxOf[[]byte](config.ChanxInitCap)
	go logSender()
}

// 定时推送日志到日志收集接口
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
				postLog(cfg.PostAPI, bb)
				bb = buffer.Get()
				num = 0
			}
		case bs, ok := <-LogChan.Out:
			if !ok {
				if bb.Len() > 0 {
					postLog(cfg.PostAPI, bb)
				}
				Log.Warn().Msg("logSender exited")
				return
			}

			// ,{"json":"..."}
			_ = bb.WriteByte(',')
			_, _ = bb.Write(bs)

			// 按条数或内容大小分批发送
			num++
			if num%cfg.PostBatchNum == 0 || bb.Len() > cfg.PostBatchBytes {
				postLog(cfg.PostAPI, bb)
				bb = buffer.Get()
				num = 0
			}
		}
	}
}

func postLog(api string, bb *buffer.Buffer) {
	if api == "" {
		return
	}

	// [{"json":"..."},{"json":"..."}]
	bb.B[0] = '['
	_ = bb.WriteByte(']')

	// 推送日志数据到接口 POST JSON
	_ = ants.Submit(func() {
		defer bb.Put()
		if _, err := req.SetBodyJsonBytes(bb.B).Post(api); err != nil {
			LogSampled.Warn().Err(err).Str("url", api).Msg("postLog")
		}
	})
}
