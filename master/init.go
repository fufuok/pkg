package master

import (
	"context"
	"log"
	"os"

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
	initConfigPipelines = append([]StageFunc{config.Start}, initConfigPipelines...)
	initPipelines = append([]StageFunc{common.Start, crontab.Start, startAddon}, initPipelines...)
	runtimeConfigPipelines = append([]StageFunc{config.Runtime}, runtimeConfigPipelines...)
	runtimePipelines = append([]StageFunc{common.Runtime, crontab.Runtime, runtimeAddon}, runtimePipelines...)
	Register(StopStage, stopAddon, crontab.Stop, common.Stop, config.Stop)
}

func startAddon() error {
	err := startTimeSync()
	return err
}

func runtimeAddon() error {
	err := runtimeTimeSync()
	return err
}

func stopAddon() error {
	err := stopTimeSync()
	return err
}

// 程序配置初始化入口
func startConfigPipeline() {
	if err := runPipelines(ConfigStage); err != nil {
		log.Fatalln("Failed to initialize config:", err, "\nbye.")
	}
}

// 程序初始化入口
func startPipeline() {
	if err := runPipelines(InitStage); err != nil {
		log.Fatalln("Failed to initialize main:", err, "\nbye.")
	}
	// 程序和配置监控
	go mainScheduler()
}

// 配置变化时先加载新配置
func runtimeConfigPipeline() {
	if err := runPipelines(RuntimeConfigStage); err != nil {
		alarm.Error().Err(err).Msg("runtime config pipeline")
	}
}

// 配置变化时运行
func runtimePipeline() {
	if err := runPipelines(RuntimeStage); err != nil {
		alarm.Error().Err(err).Msg("runtime main pipeline")
	}
}

// 程序退出时清理
func stopPipeline() {
	if err := runPipelines(StopStage); err != nil {
		logger.Fatal().Err(err).Str("app", config.AppName).Msg("Main exited")
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
			logger.Warn().Msg("restart <-restartChan")
			os.Exit(0)
		case <-reloadChan:
			// 重载配置及相关服务
			cancel()
			logger.Warn().Msg("reload <-reloadChan")
		}
	}
}
