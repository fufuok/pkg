package master

import (
	"flag"
	"fmt"
	"runtime"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xdaemon"

	"github.com/fufuok/pkg/config"
)

var (
	// FlagParser 可由 App 指定自定义的 flag
	FlagParser = flagParse

	Daemon  bool
	Version bool
)

func flagParse() {
	flag.StringVar(&config.RootPath, "p", config.DefaultRootPath, "程序启动目录(可选)")
	flag.StringVar(&config.ConfigFile, "c", config.ConfigFile, "主配置文件绝对路径(可选)")
	flag.BoolVar(&config.Debug, "debug", config.Debug, "开发者调试模式, 控制台 Debug 日志")
	flag.BoolVar(&Daemon, "d", Daemon, "启动后台守护进程")
	flag.BoolVar(&Version, "v", Version, "版本信息")
	flag.Parse()
}

// Main 带默认命令行参数启动
func Main() {
	FlagParser()

	if Version {
		fmt.Println(">>>", config.AppName, config.Version, config.GoVersion)
		fmt.Println(">>>", config.GitCommit)
		return
	}

	Run()
}

// Run 守护进程启动程序
func Run() {
	if Daemon && !config.Debug {
		xdaemon.NewDaemon(config.LogDaemon).Run()
	}

	// 手动设置 > 1, 避免 CPU 隔离时协程池调度可能的阻塞
	runtime.GOMAXPROCS(config.DefaultGOMAXPROCS)

	Start()
	defer Stop()

	utils.WaitSignal()
}

// Start 执行程序初始化
func Start() {
	registerCommonFuncs()
	registerPipeline()
	startConfigPipeline()
	startPipeline()
}

// Stop 程序退出
func Stop() {
	stopPipeline()
}
