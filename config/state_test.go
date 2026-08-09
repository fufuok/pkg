package config

import (
	"maps"
	"os"
	"slices"
	"testing"
)

// preserveConfigPackageState 保存 config 测试会触达的全部进程级状态.
//
// config 的解析函数会发布配置指针、路径、名单、密钥派生值和 env 跟踪集合.
// 测试必须在修改前调用本函数, 并且不得并行运行; 恢复通过 t.Cleanup 注册,
// 即使用例提前 Fatal 也不会把状态泄漏给后续测试.
func preserveConfigPackageState(t *testing.T) {
	t.Helper()
	if testState != nil {
		t.Fatal("config test helper must be stopped before preserving package state")
	}

	state := captureConfigTesterState()
	debug := Debug
	logDaemon := LogDaemon
	debVersion := DebVersion
	defaultGOMAXPROCS := DefaultGOMAXPROCS
	logLevel := LogLevel
	logSamplePeriodDur := LogSamplePeriodDur
	logSampleBurst := LogSampleBurst
	logFileMaxSize := LogFileMaxSize
	logFileMaxBackups := LogFileMaxBackups
	logFileMaxAge := LogFileMaxAge
	logPostIntervalDuration := LogPostIntervalDuration
	logPostBatchNum := LogPostBatchNum
	logPostBatchBytes := LogPostBatchBytes
	watcherIntervalDuration := WatcherIntervalDuration
	defaultLoadConfigInterval := DefaultLoadConfigInterval
	defaultRandomWait := DefaultRandomWait
	reqTimeoutDuration := ReqTimeoutDuration
	reqTimeoutShortDuration := ReqTimeoutShortDuration
	chanxInitCap := ChanxInitCap
	chanxMaxBufCap := ChanxMaxBufCap
	webServerAddr := WebServerAddr
	webServerHTTPSAddr := WebServerHttpsAddr
	webSignTTLDefault := WebSignTTLDefault
	webSignTTLMin := WebSignTTLMin
	webBodyLimit := WebBodyLimit

	t.Cleanup(func() {
		// 用例若启动了公开测试助手但未主动停止, 先释放其临时目录和状态所有权.
		if testState != nil {
			StopTester()
		}

		AppBaseSecretValue = state.appBaseSecretValue
		AppConfigBody = slices.Clone(state.appConfigBody)
		ConfigInitialized = state.configInitialized
		BinName = state.binName
		AppName = state.appName
		DebName = state.debName
		RootPath = state.rootPath
		DefaultLogPath = state.defaultLogPath
		LogPath = state.logPath
		LogFile = state.logFile
		ConfigPath = state.configPath
		ConfigFile = state.configFile
		EnvFilePath = state.envFilePath
		EnvMainFile = state.envMainFile
		WhitelistConfigFile = state.whitelistConfigFile
		DefaultWhitelistConfigFile = state.defaultWhitelistConfigFile
		BlacklistConfigFile = state.blacklistConfigFile
		DefaultBlacklistConfigFile = state.defaultBlacklistConfigFile
		ReqUserAgent = state.reqUserAgent
		BaseSecretValue = state.baseSecretValue
		BaseSecretEnvName = state.baseSecretEnvName
		BaseSecretSalt = state.baseSecretSalt
		BaseSecretKeyValue = state.baseSecretKeyValue
		WebTokenSalt = state.webTokenSalt
		NodeInfoFile = state.nodeInfoFile
		NodeInfoBackupFile = state.nodeInfoBackupFile
		NodeIPFromAPI = state.nodeIPFromAPI
		nodeIPFetcherRunning = state.nodeIPFetcherRunning
		AlarmOn.Store(state.alarmOn)
		mainConf.Store(state.mainConf)
		Whitelist = state.whitelist
		Blacklist = state.blacklist
		extraEnvFiles = slices.Clone(state.extraEnvFiles)
		envFileKeys = maps.Clone(state.envFileKeys)
		restoreTesterEnvironment(state.environment)

		Debug = debug
		LogDaemon = logDaemon
		DebVersion = debVersion
		DefaultGOMAXPROCS = defaultGOMAXPROCS
		LogLevel = logLevel
		LogSamplePeriodDur = logSamplePeriodDur
		LogSampleBurst = logSampleBurst
		LogFileMaxSize = logFileMaxSize
		LogFileMaxBackups = logFileMaxBackups
		LogFileMaxAge = logFileMaxAge
		LogPostIntervalDuration = logPostIntervalDuration
		LogPostBatchNum = logPostBatchNum
		LogPostBatchBytes = logPostBatchBytes
		WatcherIntervalDuration = watcherIntervalDuration
		DefaultLoadConfigInterval = defaultLoadConfigInterval
		DefaultRandomWait = defaultRandomWait
		ReqTimeoutDuration = reqTimeoutDuration
		ReqTimeoutShortDuration = reqTimeoutShortDuration
		ChanxInitCap = chanxInitCap
		ChanxMaxBufCap = chanxMaxBufCap
		WebServerAddr = webServerAddr
		WebServerHttpsAddr = webServerHTTPSAddr
		WebSignTTLDefault = webSignTTLDefault
		WebSignTTLMin = webSignTTLMin
		WebBodyLimit = webBodyLimit
	})
}

// clearTestEnvironment 删除环境变量并在测试结束时恢复其原始存在性和值.
// 需要验证 godotenv 对“变量不存在”语义的测试使用本函数, 其他场景优先 t.Setenv.
func clearTestEnvironment(t *testing.T, key string) {
	t.Helper()
	old, exists := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset test environment %s: %v", key, err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
