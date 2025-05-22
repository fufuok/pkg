package master

import (
	"log"
	"time"

	"github.com/fufuok/cache/xsync"
	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xhash"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/logger"
)

var (
	// ConfigModTime 公共配置文件集最后修改时间
	ConfigModTime time.Time

	// ConfigLoadTime 配置文件最后加载时间
	ConfigLoadTime time.Time

	// watchers 自助添加的文件变化监控器
	watchers = xsync.NewMap[string, Watcher]()

	// 存放监控器标识和对应的 MD5
	watcherMD5 = xsync.NewMap[string, string]()

	isRuntime bool
	mainFile  string

	// MainWatcherKey 主程序二进制和配置文件监控器标识
	MainWatcherKey     = "__PKG_MAIN_BIN_WATCHER__"
	MainWatcherConfKey = "__PKG_MAIN_CONFIG_WATCHER__"

	// 待监控内容变化的额外文件列表
	extraWatcherFiles []string
)

// Watcher 文件变化监控器
type Watcher struct {
	// 监控器标识
	Key string

	// 待监控的文件列表
	Files []string

	// 文件列表中文件内容变化时执行
	Func func()

	// 始终执行, 不关注文件内容是否变化
	Always bool

	// 基于文件内容是否变化的标记生成函数, 默认为: MD5Files
	HashGenerator func(...string) string
}

func (w Watcher) Start() {
	if w.Key == "" || w.Func == nil {
		return
	}

	if w.HashGenerator == nil {
		w.HashGenerator = MD5Files
	}

	md5 := w.HashGenerator(w.Files...)
	watcherMD5.Store(w.Key, md5)
	watchers.Store(w.Key, w)
	if isRuntime {
		logger.Warn().Str("key", w.Key).Strs("files", w.Files).Msg("Watcher started")
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
	ConfigModTime = common.GTimeNow()
	watcherMD5.Store(MainWatcherKey, md5Main)
	watcherMD5.Store(MainWatcherConfKey, md5Conf)

	config.DebVersion = getCurrentDebVersion()
	logger.Warn().Str("main", mainFile).Str(config.DebName, config.DebVersion).Msg("Watching")
	logger.Warn().Strs("configs", confFiles).Msg("Watching")
	logger.Warn().RawJSON("data", json.MustJSON(config.Config().NodeConf.NodeInfo)).Msg("Node info updated")

	var keys []string
	watchers.Range(func(key string, _ Watcher) bool {
		keys = append(keys, key)
		return true
	})
	if len(keys) > 0 {
		logger.Warn().Strs("keys", keys).Msg("Watching")
	}

	go startWatcher()
}

// 监听程序二进制变化(重启)和配置文件(热加载)
func startWatcher() {
	cfg := config.Config().SYSConf
	interval := cfg.WatcherIntervalDuration
	logger.Warn().Int("count", watchers.Size()+2).Str("interval", interval.String()).Msg("Watching")

	ConfigLoadTime = common.GTimeNow()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if c := mainWatcher(); c {
			continue
		}

		appWatcher()

		if c := configWatcher(); c {
			continue
		}

		// 第一时间加载新配置
		runtimeConfigPipeline()
		cfg = config.Config().SYSConf

		// 同步更新机器上现在的包版本
		config.DebVersion = getCurrentDebVersion()
		if c := checkUpgradeOrRestart(cfg); c {
			continue
		}

		runtimePipeline()
		ConfigLoadTime = common.GTimeNow()

		// 更新配置文件监控周期
		if interval != cfg.WatcherIntervalDuration {
			interval = cfg.WatcherIntervalDuration
			ticker.Reset(interval)
			logger.Warn().Str("interval", interval.String()).Msg("Main watcher interval updated")
		}

		// 最终的配置列表和 MD5
		md5ok, confFiles := MD5ConfigFiles()
		watcherMD5.Store(MainWatcherConfKey, md5ok)
		logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> Reload config <<<<<<<")
		logger.Warn().Strs("configs", confFiles).Msg("Watching")
		logger.Warn().RawJSON("data", json.MustJSON(config.Config().NodeConf.NodeInfo)).Msg("Node info updated")
		logger.Warn().Int("count", watchers.Size()+2).Str("interval", interval.String()).Msg("Watching")
		reloadChan <- true
	}
}

func mainWatcher() (needContinue bool) {
	// 程序二进制变化时重启
	md5New := MD5Files(mainFile)
	md5Main, _ := watcherMD5.LoadAndStore(MainWatcherKey, md5New)
	if md5New == md5Main {
		return
	}
	logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> Restart main <<<<<<<")
	restartChan <- true
	return true
}

// 运行应用自助添加的监控器
func appWatcher() {
	watchers.Range(func(k string, w Watcher) bool {
		md5New := w.HashGenerator(w.Files...)
		md5Old, _ := watcherMD5.LoadAndStore(k, md5New)
		if w.Always || md5New != md5Old {
			utils.SafeGo(w.Func)
		}
		return true
	})
}

func configWatcher() (needContinue bool) {
	// 系统配置检查和重载
	md5New, _ := MD5ConfigFiles()
	md5Conf, _ := watcherMD5.LoadAndStore(MainWatcherConfKey, md5New)
	if md5New == md5Conf {
		return true
	}
	ConfigModTime = common.GTimeNow()

	// 任意配置文件变化, 热加载所有配置
	if err := config.LoadConfig(); err != nil {
		logger.Error().Err(err).Msg("Failed to reload config")
		return true
	}
	return false
}

func checkUpgradeOrRestart(cfg config.SYSConf) (needContinue bool) {
	// 安装新版本, 每当配置有变化时才检测
	if cfg.DebVersion != "" && config.DebVersion != "" && config.DebVersion != cfg.DebVersion {
		threshold := cfg.CanaryDeployment
		toInstall := canary(cfg.DebVersion, threshold)
		logger.Warn().
			Strs("deb_versions", []string{config.DebVersion, cfg.DebVersion}).
			Bool("to_install", toInstall).Str("ip", common.ExternalIPv4).Uint64("threshold", threshold).
			Msg("Starting canary deployment")
		if toInstall {
			go installDeb(cfg.DebVersion)
		}
	}

	// 重启程序指令
	if cfg.RestartMain {
		logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> Restart main(config) <<<<<<<")
		restartChan <- true
		return true
	}
	return
}

// SetExtraWatcherFiles 设置额外的文件到内容变化监控列表
func SetExtraWatcherFiles(confFile ...string) {
	extraWatcherFiles = confFile
}

// MD5ConfigFiles 配置文件 MD5, 有变化时重载系统配置项
func MD5ConfigFiles() (md5 string, confFiles []string) {
	confFiles = append(confFiles, config.ConfigFile, config.WhitelistConfigFile, config.BlacklistConfigFile)
	confFiles = append(confFiles, config.GetEnvFiles()...)
	confFiles = append(confFiles, extraWatcherFiles...)
	if config.NodeInfoFile != "" {
		confFiles = append(confFiles, config.NodeInfoFile)
	}
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
