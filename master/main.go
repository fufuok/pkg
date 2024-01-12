package master

import (
	"flag"
	"fmt"
	"runtime"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xdaemon"

	"github.com/fufuok/pkg/config"
)

var version, daemon bool

func flagParse() {
	flag.StringVar(&config.RootPath, "p", config.DefaultRootPath, "程序启动目录(可选)")
	flag.StringVar(&config.ConfigFile, "c", config.ConfigFile, "主配置文件绝对路径(可选)")
	flag.BoolVar(&config.Debug, "debug", false, "全局调试模式, 控制台 DEBUG 日志")
	flag.BoolVar(&daemon, "d", false, "启动后台守护进程")
	flag.BoolVar(&version, "v", false, "版本信息")
	flag.Parse()
}

// Main 带默认命令行参数启动
func Main() {
	flagParse()

	if version {
		fmt.Println(">>>", config.AppName, config.Version, config.GoVersion)
		fmt.Println(">>>", config.GitCommit)
		return
	}

	Run()
}

func Run() {
	if daemon && !config.Debug {
		xdaemon.NewDaemon(config.LogDaemon).Run()
	}

	// 手动设置 > 1, 避免 CPU 隔离时协程池调度可能的阻塞
	runtime.GOMAXPROCS(config.DefaultGOMAXPROCS)

	registerCommonFuncs()
	registerPipeline()
	startConfigPipeline()
	startPipeline()
	defer stopPipeline()

	utils.WaitSignal()
}
