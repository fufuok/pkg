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
	LogChan *chanx.UnboundedChan[[]byte]

	// 日志发送接口地址
	postAPI string
)

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
	postAPI = cfg.PostAPI
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
				postAPI = cfg.PostAPI
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
				Log.Warn().Msg("Log sender exited")
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

func postLog(bb *buffer.Buffer) {
	if postAPI == "" {
		bb.Put()
		return
	}

	// [{"json":"..."},{"json":"..."}]
	bb.B[0] = '['
	_ = bb.WriteByte(']')

	// 推送日志数据到接口 POST JSON
	_ = ants.Submit(func() {
		defer bb.Put()
		if _, err := req.SetBodyJsonBytes(bb.B).Post(postAPI); err != nil {
			LogSampled.Warn().Err(err).Str("api", postAPI).Msg("Posting log")
		}
	})
}

// PostLog 立即推送日志到 ES
func PostLog(bs []byte) {
	if postAPI == "" {
		return
	}

	// 推送日志数据到接口 POST JSON
	_ = ants.Submit(func() {
		if _, err := req.SetBodyJsonBytes(bs).Post(postAPI); err != nil {
			LogSampled.Warn().Err(err).Str("api", postAPI).Msg("Posting log")
		}
	})
}
