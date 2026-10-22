package master

import (
	"flag"
	"fmt"
	"log"
	"runtime"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xdaemon"

	"github.com/fufuok/pkg/common"
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

// Run 在启动 daemon 和业务前固定应用时区, 再运行完整程序生命周期.
func Run() {
	if err := common.InitTimeLocation(); err != nil {
		log.Fatalln("Failed to initialize application time location:", err)
	}
	if Daemon && !config.Debug {
		xdaemon.NewDaemon(config.LogDaemon).Run()
	}

	// 只在应用显式设置了不同的值时才覆盖运行时, 默认保留随 cgroup 限额的自动更新
	applyGOMAXPROCS(config.DefaultGOMAXPROCS)

	Start()
	defer Stop()

	utils.WaitSignal()
}

// applyGOMAXPROCS 在 n 大于 0 且不同于当前值时设置运行时 GOMAXPROCS, 返回是否执行了设置.
//
// Go 1.25 起任何 runtime.GOMAXPROCS(n>0) 调用, 即使值不变, 都会关闭运行时随 cgroup CPU 限额
// 变化的自动更新, 因此与当前值相同时必须跳过. n 小于等于 0 表示不覆盖. 一旦执行了设置,
// 本进程不再随限额变化自动调整, 这是应用显式指定 DefaultGOMAXPROCS 的代价.
func applyGOMAXPROCS(n int) bool {
	if n <= 0 || n == runtime.GOMAXPROCS(0) {
		return false
	}
	runtime.GOMAXPROCS(n)
	return true
}

// Start 在任何 Pipeline 创建后台任务前固定时区, 也支持不经 Main/Run 的直接启动.
func Start() {
	if err := common.InitTimeLocation(); err != nil {
		log.Fatalln("Failed to initialize application time location:", err)
	}
	registerCommonFuncs()
	registerPipeline()
	startConfigPipeline()
	loadBinaryChangeAction()
	startPipeline()
}

// Stop 取消安装器的本地等待后退出业务, 不等待或强杀已运行的包管理命令.
func Stop() {
	debInstall.stop()
	stopPipeline()
}
