package master

import (
	"context"
	"time"

	"github.com/fufuok/pkg/utils"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/pools/timerpool"
)

var (
	// remoteWait 远端拉取循环中的等待, true 表示应继续下一轮.
	// 默认等待可被 ctx 取消; 包内测试可替换以钉住取消窗口, 不支持并行改写.
	remoteWait = waitRemote
	// remoteRandomWaitSeconds 首次抖动秒数, 默认 FastIntn(RandomWait).
	remoteRandomWaitSeconds = func(n int) int {
		return utils.FastIntn(n)
	}
)

// waitRemote 等待 d, 到期返回 true, ctx 取消返回 false.
// d <= 0 时仍先观察一次取消, 避免热更新后旧协程立刻再拉一轮.
func waitRemote(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := timerpool.New(d)
	defer timerpool.Release(timer)
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// 初始化获取远端配置
func startRemotePipelines(ctx context.Context) {
	// 定时获取远程主配置, 黑白名单配置
	getMainRemoteConf(ctx)
	getWhitelistRemoteConf(ctx)
	getBlacklistRemoteConf(ctx)

	// 运行应用级自定义的获取远端方法
	ps := getPipelinesWithContext(RemoteStage)
	for _, sf := range ps {
		sf(ctx)
	}
	logger.Warn().Int("count", len(ps)+3).Msg("Remote config fetchers started")
}

func getMainRemoteConf(ctx context.Context) {
	cfg := config.Config().MainConf
	if cfg.GetConfDuration <= 0 {
		return
	}
	GetRemoteConf(ctx, cfg)
}

func getWhitelistRemoteConf(ctx context.Context) {
	cfg := config.Config().WhitelistConf
	if cfg.GetConfDuration <= 0 {
		return
	}
	GetRemoteConf(ctx, cfg)
}

func getBlacklistRemoteConf(ctx context.Context) {
	cfg := config.Config().BlacklistConf
	if cfg.GetConfDuration <= 0 {
		return
	}
	GetRemoteConf(ctx, cfg)
}

// GetRemoteConf 定时获取远端配置, 配合 RemotePipelines 使用.
// 主配置变化时旧循环应在随机等待和周期等待期间响应 ctx 取消并退出, 避免与新 fetcher 重叠写同一文件.
func GetRemoteConf(ctx context.Context, cfg config.FilesConf) {
	id := common.GTimeNowString("060102150405.999999999")
	logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
		Msg("Remote config fetcher started")
	fetcher := func() {
		for {
			wait := remoteRandomWaitSeconds(cfg.RandomWait)
			if !remoteWait(ctx, time.Duration(wait)*time.Second) {
				logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
					Msg("Remote config fetcher exited")
				return
			}
			select {
			case <-ctx.Done():
				logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
					Msg("Remote config fetcher exited")
				return
			default:
			}
			// 是否跳过更新远端配置
			if !config.IsSkipRemoteConfig() {
				if err := common.InvokeConfigMethod(cfg); err != nil {
					sampler.Error().Err(err).Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
						Msg("Failed to get remote config")
				} else {
					logger.Info().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
						Msg("Execute remote config fetcher")
				}
			}
			if !remoteWait(ctx, cfg.GetConfDuration) {
				logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
					Msg("Remote config fetcher exited")
				return
			}
		}
	}
	utils.SafeGo(fetcher, common.RecoverAlarm)
}
