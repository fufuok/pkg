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

	// pipelineRuntimeErrorEvent 保留生产报警事件来源, 供包内测试隔离全局 logger.
	pipelineRuntimeErrorEvent = alarm.Error
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
		if _, builtin := p.(*config.M); builtin {
			// 在业务启动前固定自包名和版本快照, 此处不触发安装.
			prepareDebInstaller()
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
	// 程序和配置监控; 闸门先发布再启动 scheduler, 使启动后任意时刻的 Stop 都能约束 watcher.
	g := newReloadGate()
	currentGate.Store(g)
	go mainScheduler(g)
}

// 配置变化时先加载新配置; 已请求停机时跳过其余模块 (它们可能已被 Runtime 内重入的 Stop 关闭).
func runtimeConfigPipeline() {
	ps := getPipelines(ConfigStage)
	for _, p := range ps {
		if stopRequested() {
			return
		}
		if err := p.Runtime(); err != nil {
			pipelineRuntimeErrorEvent().Err(err).Msg("Runtime config pipeline failed")
		}
	}
}

// 配置变化时运行; 已请求停机时跳过其余模块, 同 runtimeConfigPipeline.
func runtimePipeline() {
	ps := getPipelines(MainStage)
	for _, p := range ps {
		if stopRequested() {
			return
		}
		if err := p.Runtime(); err != nil {
			pipelineRuntimeErrorEvent().Err(err).Msg("Runtime main pipeline failed")
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

// mainScheduler 启动 watcher 并按重启/重载信号调度远端拉取任务, g 停机后取消远端任务并退出.
func mainScheduler(g *reloadGate) {
	initWatcher(g)
	for {
		ctx, cancel := context.WithCancel(context.Background())

		// 获取远程配置, 配置重载时开启新任务
		go startRemotePipelines(ctx)

		select {
		case <-g.quit:
			// 已停机且没有在途周期: 远端任务不再拉取, scheduler 退出.
			cancel()
			return
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
