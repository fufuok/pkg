package master

import (
	"context"
	"time"

	"github.com/fufuok/utils"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/sampler"
)

// 初始化获取远端配置
func startRemotePipelines(ctx context.Context) {
	// 定时获取远程主配置, 黑白名单配置
	utils.SafeGoWithContext(ctx, getMainRemoteConf, common.RecoverAlarm)
	utils.SafeGoWithContext(ctx, getWhitelistRemoteConf, common.RecoverAlarm)
	utils.SafeGoWithContext(ctx, getBlacklistRemoteConf, common.RecoverAlarm)

	// 运行应用级自定义的获取远端方法
	ps := getPipelinesWithContext(RemoteStage)
	for _, sf := range ps {
		sf := sf
		utils.SafeGoWithContext(ctx, sf, common.RecoverAlarm)
	}
	logger.Warn().Int("count", len(ps)+3).Msg("Remote configuration fetcher")
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

// GetRemoteConf 定时获取远端配置, 配合 RemotePipelines 使用
// 注: 当主配置变化时, 该函数会退出并重新运行
func GetRemoteConf(ctx context.Context, cfg config.FilesConf) {
	id := common.GTimeNowString("060102150405.999999999")
	logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
		Msg("Remote configuration fetcher is working")

	for {
		wait := utils.FastIntn(cfg.RandomWait)
		time.Sleep(time.Duration(wait) * time.Second)
		select {
		case <-ctx.Done():
			logger.Warn().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
				Msg("Remote configuration fetcher exited")
			return
		default:
		}
		// 是否跳过更新远端配置
		if !config.IsSkipRemoteConfig() {
			if err := common.InvokeConfigMethod(cfg); err != nil {
				sampler.Error().Err(err).Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
					Msg("Failed to get remote configuration")
			} else {
				logger.Info().Str("id", id).Str("path", cfg.Path).Str("method", cfg.Method).
					Msg("Execute remote configuration fetcher")
			}
		}
		time.Sleep(cfg.GetConfDuration)
	}
}
