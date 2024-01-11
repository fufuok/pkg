package crontab

import (
	"github.com/fufuok/cron"
	"github.com/fufuok/utils/xsync"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	// 定时任务调度器
	crontab *cron.Cron
)

// Start 程序启动时初始化
func Start() error {
	initMain()
	return nil
}

// Runtime 重新加载配置时运行
func Runtime() error {
	return nil
}

func Stop() error {
	crontab.Stop()
	logger.Warn().Msg("Crontab exited")
	return nil
}

// 初始化定时任务环境
func initMain() {
	jobs = xsync.NewMapOf[string, *Job]()
	crontab = cron.New(
		cron.WithLocation(config.DefaultTimeLocation),
		cron.WithSecondOptional(),
		cron.WithChain(
			cron.Recover(common.NewCronLogger()),
		),
	)
	crontab.Start()
	logger.Info().Msg("crontab is working")
}
