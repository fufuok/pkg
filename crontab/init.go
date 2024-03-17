package crontab

import (
	"github.com/fufuok/cron"
	"github.com/fufuok/utils/xsync"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

// 定时任务调度器
var crontab *cron.Cron

type M struct{}

// Start 程序启动时初始化
func (m *M) Start() error {
	initMain()
	return nil
}

// Runtime 重新加载配置时运行
func (m *M) Runtime() error {
	return nil
}

// Stop 程序退出时运行
func (m *M) Stop() error {
	crontab.Stop()
	logger.Warn().Msg("Crontab exited")
	return nil
}

// 初始化定时任务环境
func initMain() {
	jobs = xsync.NewMapOf[string, *Job]()
	opts := []cron.Option{
		cron.WithLocation(config.DefaultTimeLocation),
		cron.WithSecondOptional(),
		cron.WithChain(
			cron.Recover(common.NewCronLogger()),
		),
		cron.WithCustomTime(common.GTimeNow),
	}
	if config.Debug {
		opts = append(opts, cron.WithLogger(common.NewCronLogger()))
	}
	crontab = cron.New(opts...)
	crontab.Start()
	logger.Info().Msg("crontab is working")
}
