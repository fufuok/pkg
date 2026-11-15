package common

import (
	"context"
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

	// logSenderDone 在 sender 退出 (末批已提交给协程池) 后关闭, 供 Stop 等待; 未启动 sender 时为 nil.
	logSenderDone chan struct{}

	// logTailPostTimeout 是停机时末批日志 HTTP 发送 (含重试) 的总时长上限. 默认池 Release 会等待该发送,
	// 日志接口慢而未断时不加上限会拖过常见 5 秒的 systemd 停止期限; 超时即放弃末批. 包内测试可调整.
	logTailPostTimeout = time.Second
)

// initLogSender 创建日志队列并启动 sender, sender 退出时关闭 logSenderDone.
func initLogSender() {
	LogChan = NewChanx[[]byte](config.ChanxInitCap)
	done := make(chan struct{})
	logSenderDone = done
	go func() {
		defer close(done)
		logSender()
	}()
}

// 定时推送日志到日志收集接口
//
//nolint:cyclop
func logSender() {
	num := 0
	bb := buffer.Get()
	// 最后一个空批次仍归 sender 所有; 非空批次交给 postLog 后清空引用.
	defer func() {
		if bb != nil {
			bb.Put()
		}
	}()
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
					// 队列已关闭 (停机): 末批限时发送, 避免拖住停机.
					postLogTimeout(bb, logTailPostTimeout)
					bb = nil
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
	postLogTimeout(bb, 0)
}

// postLogTimeout 同 postLog, timeout 大于 0 时限制本次发送 (含重试) 的总时长, 否则使用客户端默认超时.
func postLogTimeout(bb *buffer.Buffer, timeout time.Duration) {
	api := config.Config().LogConf.PostAPI
	if api == "" {
		bb.Put()
		return
	}

	// [{"json":"..."},{"json":"..."}]
	bb.B[0] = '['
	_ = bb.WriteByte(']')

	submitLogTimeout(api, bb.B, bb.Put, timeout)
}

// PostLog 异步推送日志, 使用调用时已发布的接口地址, 不依赖 sender 的周期配置刷新.
func PostLog(bs []byte) {
	api := config.Config().LogConf.PostAPI
	if api == "" {
		return
	}

	submitLog(api, bs, nil)
}

// submitLog 转交正文的生命周期; 提交成功由后台任务回收资源, 提交被池拒绝则由调用方立即回收.
// release 至多执行一次. 诊断仅写本地采样日志, 避免远端故障递归放大.
func submitLog(api string, bs []byte, release func()) {
	submitLogTimeout(api, bs, release, 0)
}

// submitLogTimeout 同 submitLog, timeout 大于 0 时以带期限的 Context 发送; 默认重试退避响应 Context, 期限覆盖重试.
func submitLogTimeout(api string, bs []byte, release func(), timeout time.Duration) {
	err := ants.Submit(func() {
		if release != nil {
			defer release()
		}
		r := req.SetBodyJsonBytes(bs)
		if timeout > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			r.SetContext(ctx)
		}
		resp, err := r.Post(api)
		if err != nil {
			LogSampled().Warn().Err(err).Str("api", api).Msg("Posting log")
			return
		}
		if !resp.IsSuccessState() {
			LogSampled().Warn().Int("status", resp.StatusCode).Str("api", api).Msg("Posting log")
		}
	})
	if err != nil {
		if release != nil {
			release()
		}
		LogSampled().Warn().Err(err).Str("api", api).Msg("Submitting log")
	}
}
