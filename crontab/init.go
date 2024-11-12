package crontab

import (
	"sync/atomic"

	"github.com/fufuok/cron"
	"github.com/fufuok/utils/xsync"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

var (
	// 定时任务调度器
	crontab *cron.Cron

	// 默认 false: 按调度时间执行(允许重叠执行任务)
	// true: 每任务单例执行, 上一任务未完成时, 跳过此次执行机会
	skipIfStillRunning atomic.Bool
)

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

// SetSkipIfStillRunning 全局设置任务是否单例执行
func SetSkipIfStillRunning(v bool) {
	skipIfStillRunning.Store(v)
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
