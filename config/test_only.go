package config

import (
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var (
	testModule M
	testState  *configTesterState
	testConfig = []byte(`{
"log_conf": {
    "level": 0,
    "post_api_env": "POST_API",
    "post_alarm_api_env": "POST_ALARM_API",
    "alarm_code_env": "ALARM_CODE",
    "post_interval": 7
  }
}`)
)

// configTesterState 保存 InitTester 实际触达的全部包级状态.
//
// 助手只支持串行 Init-Stop 生命周期. 保存引用即可恢复 mainConf 和名单的发布状态,
// 初始化期间生成的新对象由临时根目录和当前配置指针统一回收.
type configTesterState struct {
	appBaseSecretValue string
	appConfigBody      []byte
	configInitialized  bool

	binName string
	appName string
	debName string

	rootPath                   string
	defaultLogPath             string
	logPath                    string
	logFile                    string
	configPath                 string
	configFile                 string
	envFilePath                string
	envMainFile                string
	whitelistConfigFile        string
	defaultWhitelistConfigFile string
	blacklistConfigFile        string
	defaultBlacklistConfigFile string
	reqUserAgent               string
	baseSecretValue            string
	baseSecretEnvName          string
	baseSecretSalt             string
	baseSecretKeyValue         string
	webTokenSalt               string
	nodeInfoFile               string
	nodeInfoBackupFile         string
	nodeIPFromAPI              string
	nodeIPFetcherRunning       bool
	alarmOn                    bool
	mainConf                   *MainConf
	whitelist                  map[*net.IPNet]int64
	blacklist                  map[*net.IPNet]int64
	extraEnvFiles              []string
	envFileKeys                map[string]struct{}
	environment                map[string]string
	tempRoot                   string
}

// InitTester 建立自包含、离线且可恢复的配置测试环境.
//
// 函数签名保持兼容. 调用方必须串行执行 InitTester/StopTester, 不支持嵌套或并发调用.
// 测试配置使用临时根目录和内存 JSON, 不读取生产配置文件或写入生产目录.
func InitTester() {
	if testState != nil {
		panic("config test helper is already initialized")
	}

	state := captureConfigTesterState()
	tempRoot, err := os.MkdirTemp("", "pkg-config-test-")
	if err != nil {
		panic("failed to create config test root: " + err.Error())
	}
	state.tempRoot = tempRoot
	testState = state

	// 清空可能覆盖测试配置的机器环境变量, StopTester 会按完整快照恢复.
	for _, key := range []string{
		MainConfigFileEnvName,
		MainConfigNameEnvName,
		MainConfRemoteAPIEnvName,
		BaseSecretKeyNameEnvName,
		BaseSecretSaltEnvName,
		BinNameEnvName,
		AppNameEnvName,
		DebNameEnvName,
		WebCertFileEnvName,
		WebKeyFileEnvName,
		WebSignKeyEnvName,
		NodeInfoBackupEnabledEnvName,
		"POST_API",
		"POST_ALARM_API",
		"ALARM_CODE",
	} {
		_ = os.Unsetenv(key)
	}

	AppBaseSecretValue = "Tester"
	AppConfigBody = slices.Clone(testConfig)
	ConfigInitialized = false
	RootPath = filepath.Join(tempRoot, "bin")
	DefaultLogPath = filepath.Join(tempRoot, "log")
	LogPath = ""
	LogFile = ""
	ConfigPath = ""
	ConfigFile = ""
	EnvFilePath = ""
	EnvMainFile = ""
	WhitelistConfigFile = ""
	DefaultWhitelistConfigFile = ""
	BlacklistConfigFile = ""
	DefaultBlacklistConfigFile = ""
	ReqUserAgent = ""
	BaseSecretValue = ""
	WebTokenSalt = ""
	NodeInfoFile = ""
	NodeInfoBackupFile = ""
	NodeIPFromAPI = ""
	nodeIPFetcherRunning = false
	Whitelist = nil
	Blacklist = nil
	extraEnvFiles = nil
	envFileKeys = nil
	mainConf.Store(nil)
	AlarmOn.Store(false)

	_ = testModule.Start()
}

// StopTester 恢复 InitTester 保存的配置、路径、名单和进程环境.
//
// 未初始化时调用为 no-op. 临时目录只包含本次助手创建的数据, 恢复全局状态后删除.
func StopTester() {
	state := testState
	if state == nil {
		return
	}
	testState = nil
	_ = testModule.Stop()

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
	Whitelist = state.whitelist
	Blacklist = state.blacklist
	extraEnvFiles = slices.Clone(state.extraEnvFiles)
	envFileKeys = maps.Clone(state.envFileKeys)
	mainConf.Store(state.mainConf)
	AlarmOn.Store(state.alarmOn)
	restoreTesterEnvironment(state.environment)
	_ = os.RemoveAll(state.tempRoot)
}

// captureConfigTesterState 获取配置助手启动前的完整状态快照.
func captureConfigTesterState() *configTesterState {
	return &configTesterState{
		appBaseSecretValue:         AppBaseSecretValue,
		appConfigBody:              slices.Clone(AppConfigBody),
		configInitialized:          ConfigInitialized,
		binName:                    BinName,
		appName:                    AppName,
		debName:                    DebName,
		rootPath:                   RootPath,
		defaultLogPath:             DefaultLogPath,
		logPath:                    LogPath,
		logFile:                    LogFile,
		configPath:                 ConfigPath,
		configFile:                 ConfigFile,
		envFilePath:                EnvFilePath,
		envMainFile:                EnvMainFile,
		whitelistConfigFile:        WhitelistConfigFile,
		defaultWhitelistConfigFile: DefaultWhitelistConfigFile,
		blacklistConfigFile:        BlacklistConfigFile,
		defaultBlacklistConfigFile: DefaultBlacklistConfigFile,
		reqUserAgent:               ReqUserAgent,
		baseSecretValue:            BaseSecretValue,
		baseSecretEnvName:          BaseSecretEnvName,
		baseSecretSalt:             BaseSecretSalt,
		baseSecretKeyValue:         BaseSecretKeyValue,
		webTokenSalt:               WebTokenSalt,
		nodeInfoFile:               NodeInfoFile,
		nodeInfoBackupFile:         NodeInfoBackupFile,
		nodeIPFromAPI:              NodeIPFromAPI,
		nodeIPFetcherRunning:       nodeIPFetcherRunning,
		alarmOn:                    AlarmOn.Load(),
		mainConf:                   mainConf.Load(),
		whitelist:                  Whitelist,
		blacklist:                  Blacklist,
		extraEnvFiles:              slices.Clone(extraEnvFiles),
		envFileKeys:                maps.Clone(envFileKeys),
		environment:                captureTesterEnvironment(),
	}
}

// captureTesterEnvironment 保存进程环境, 包括空值和助手未知的业务变量.
func captureTesterEnvironment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

// restoreTesterEnvironment 恢复环境快照并移除助手期间新增的变量.
func restoreTesterEnvironment(snapshot map[string]string) {
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := snapshot[key]; !exists {
			_ = os.Unsetenv(key)
		}
	}
	for key, value := range snapshot {
		_ = os.Setenv(key, value)
	}
}
