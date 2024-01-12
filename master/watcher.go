package master

import (
	"log"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xhash"
	"github.com/fufuok/utils/xsync"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/logger"
)

var (
	// watchers 自助添加的文件变化监控器
	watchers = xsync.NewMapOf[string, Watcher]()

	// 存放监控器标识和对应的 MD5
	watcherMD5 = xsync.NewMapOf[string, string]()

	isRuntime   bool
	mainFile    string
	mainKey     = "__PKG_MAIN_BIN__"
	mainConfKey = "__PKG_MAIN_CONFIG__"
)

// Watcher 文件变化监控器
type Watcher struct {
	// 监控器标识
	Key string

	// 待监控的文件列表
	Files []string

	// 文件列表中文件内容变化时执行
	Func func()
}

func (w Watcher) Start() {
	if w.Key == "" || len(w.Files) == 0 || w.Func == nil {
		return
	}
	md5 := MD5Files(w.Files...)
	watcherMD5.Store(w.Key, md5)
	watchers.Store(w.Key, w)
	if isRuntime {
		logger.Warn().Str("key", w.Key).Strs("files", w.Files).Msg("Watching")
	}
}

func (w Watcher) Stop() {
	if w.Key == "" {
		return
	}
	watchers.Delete(w.Key)
	watcherMD5.Delete(w.Key)
	if isRuntime {
		logger.Warn().Str("key", w.Key).Strs("files", w.Files).Msg("Watcher stopped")
	}
}

func initWatcher() {
	mainFile = utils.Executable(true)
	if mainFile == "" {
		log.Fatalln("Failed to initialize Watcher: miss executable", "\nbye.")
	}

	isRuntime = true
	md5Main := MD5Files(mainFile)
	md5Conf, confFiles := MD5ConfigFiles()
	watcherMD5.Store(mainKey, md5Main)
	watcherMD5.Store(mainConfKey, md5Conf)

	config.DebVersion = getCurrentDebVersion()
	logger.Warn().Str("main", mainFile).Str(config.DebName, config.DebVersion).Msg("Watching")
	logger.Warn().Strs("configs", confFiles).Msg("Watching")
	logger.Warn().RawJSON("data", json.MustJSON(config.Config().NodeConf.NodeInfo)).Msg("NodeInfo")

	var keys []string
	watchers.Range(func(key string, _ Watcher) bool {
		keys = append(keys, key)
		return true
	})
	if len(keys) > 0 {
		logger.Warn().Strs("keys", keys).Msg("Watching")
	}

	go mainWatcher()
}

// 监听程序二进制变化(重启)和配置文件(热加载)
func mainWatcher() {
	cfg := config.Config().SYSConf
	interval := cfg.WatcherIntervalDuration
	logger.Warn().Int("count", watchers.Size()+2).Str("interval", interval.String()).Msg("Watching")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		// 程序二进制变化时重启
		md5New := MD5Files(mainFile)
		md5Main, _ := watcherMD5.LoadAndStore(mainKey, md5New)
		if md5New != md5Main {
			logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> restart main <<<<<<<")
			restartChan <- true
			continue
		}

		// 运行应用自助添加的监控器
		watchers.Range(func(k string, w Watcher) bool {
			md5New := MD5Files(w.Files...)
			md5Old, _ := watcherMD5.LoadAndStore(k, md5New)
			if md5New != md5Old {
				utils.SafeGo(w.Func)
			}
			return true
		})

		// 系统配置检查和重载
		md5New, _ = MD5ConfigFiles()
		md5Conf, _ := watcherMD5.LoadAndStore(mainConfKey, md5New)
		if md5New == md5Conf {
			continue
		}

		// 任意配置文件变化, 热加载所有配置
		if err := config.LoadConf(); err != nil {
			logger.Error().Err(err).Msg("reload config")
			continue
		}

		// 第一时间加载新配置
		runtimeConfigPipeline()
		cfg = config.Config().SYSConf

		// 安装新版本, 每当配置有变化时才检测
		if cfg.DebVersion != "" && config.DebVersion != cfg.DebVersion {
			threshold := cfg.CanaryDeployment
			toInstall := canary(cfg.DebVersion, threshold)
			logger.Warn().
				Strs("deb_versions", []string{config.DebVersion, cfg.DebVersion}).
				Bool("to_install", toInstall).Str("ip", common.ExternalIPv4).Uint64("threshold", threshold).
				Msg("Canary Deployment")
			if toInstall {
				go installDeb(cfg.DebVersion)
			}
		}

		// 重启程序指令
		if cfg.RestartMain {
			logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> restart main(config) <<<<<<<")
			restartChan <- true
			continue
		}

		runtimePipeline()

		// 更新配置文件监控周期
		if interval != cfg.WatcherIntervalDuration {
			interval = cfg.WatcherIntervalDuration
			ticker.Reset(interval)
			logger.Warn().Str("interval", interval.String()).Msg("reset ticker")
		}

		_, confFiles := MD5ConfigFiles()
		logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> reload config <<<<<<<")
		logger.Warn().Strs("configs", confFiles).Msg("Watching")
		logger.Warn().RawJSON("data", json.MustJSON(config.Config().NodeConf.NodeInfo)).Msg("NodeInfo")
		logger.Warn().Int("count", watchers.Size()+2).Str("interval", interval.String()).Msg("Watching")
		reloadChan <- true
	}
}

// MD5ConfigFiles 配置文件 MD5, 有变化时重载
func MD5ConfigFiles() (md5 string, confFiles []string) {
	confFiles = append(confFiles, config.ConfigFile, config.NodeInfoFile)
	confFiles = append(confFiles, config.ExtraEnvFiles...)
	md5 = MD5Files(confFiles...)
	return
}

// MD5Files 文件 MD5
func MD5Files(files ...string) (md5 string) {
	for _, f := range files {
		md5 += xhash.MustMD5Sum(f)
	}
	md5 = xhash.MD5Hex(md5)
	return
}
