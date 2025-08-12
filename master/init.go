package master

import (
	"context"
	"log"
	"os"
	"slices"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/crontab"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/alarm"
)

var (
	// 重启信号
	restartChan = make(chan bool)

	// 配置重载信息
	reloadChan = make(chan bool)
)

// 注册常用助手函数
func registerCommonFuncs() {
	common.Funcs.Store("GetDataSource", config.GetDataSource)
}

// 注册框架级 Pipeline
func registerPipeline() {
	configPipelines = append([]Pipeline{&config.M{}, &common.M{}}, configPipelines...)
	mainPipelines = append([]Pipeline{&crontab.M{}}, mainPipelines...)
	mainPipelines = append(mainPipelines, &addons{})
}

// 程序配置初始化入口
func startConfigPipeline() {
	ps := getPipelines(ConfigStage)
	for _, p := range ps {
		if err := p.Start(); err != nil {
			log.Fatalln("Failed to initialize config:", err, "\nbye.")
		}
	}
}

// 程序初始化入口
func startPipeline() {
	ps := getPipelines(MainStage)
	for _, p := range ps {
		if err := p.Start(); err != nil {
			log.Fatalln("Failed to initialize main:", err, "\nbye.")
		}
	}
	// 程序和配置监控
	go mainScheduler()
}

// 配置变化时先加载新配置
func runtimeConfigPipeline() {
	ps := getPipelines(ConfigStage)
	for _, p := range ps {
		if err := p.Runtime(); err != nil {
			alarm.Error().Err(err).Msg("Runtime config pipeline failed")
		}
	}
}

// 配置变化时运行
func runtimePipeline() {
	ps := getPipelines(MainStage)
	for _, p := range ps {
		if err := p.Runtime(); err != nil {
			alarm.Error().Err(err).Msg("Runtime main pipeline failed")
		}
	}
}

// 程序退出时清理
func stopPipeline() {
	ps := append(getPipelines(ConfigStage), getPipelines(MainStage)...)
	slices.Reverse(ps)
	for _, p := range ps {
		if err := p.Stop(); err != nil {
			logger.Fatal().Err(err).Str("app", config.AppName).Msg("Main exited")
		}
	}
	logger.Warn().Str("app", config.AppName).Msg("Main exited")
}

func mainScheduler() {
	initWatcher()
	for {
		ctx, cancel := context.WithCancel(context.Background())

		// 获取远程配置, 配置重载时开启新任务
		go startRemotePipelines(ctx)

		select {
		case <-restartChan:
			// 强制退出, 由 Daemon 重启程序
			logger.Warn().Msg("Restart <-restartChan")
			os.Exit(0)
		case <-reloadChan:
			// 重载配置及相关服务
			cancel()
			logger.Warn().Msg("Reload <-reloadChan")
		}
	}
}
