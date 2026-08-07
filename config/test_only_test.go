package config

import (
	"errors"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestTesterLifecycle 验证配置助手可重复执行并完整恢复调用前状态.
func TestTesterLifecycle(t *testing.T) {
	const unrelatedEnvName = "PKG_CONFIG_TEST_UNRELATED"
	ownedValueEnvName := MainConfigFileEnvName
	ownedEmptyEnvName := MainConfigNameEnvName
	ownedAbsentEnvName := MainConfRemoteAPIEnvName
	for _, key := range []string{ownedValueEnvName, ownedEmptyEnvName, ownedAbsentEnvName, unrelatedEnvName} {
		preserveTestEnvironment(t, key)
	}

	originalConfig := Config()
	originalInitialized := ConfigInitialized
	originalPaths := captureTesterPathState()
	originalWhitelist, originalBlacklist := Whitelist, Blacklist
	originalExtraEnvFiles, originalEnvFileKeys := extraEnvFiles, envFileKeys
	t.Cleanup(func() {
		StopTester()
		mainConf.Store(originalConfig)
		ConfigInitialized = originalInitialized
		restoreTesterPathState(originalPaths)
		Whitelist, Blacklist = originalWhitelist, originalBlacklist
		extraEnvFiles, envFileKeys = originalExtraEnvFiles, originalEnvFileKeys
	})

	callerConfig := &MainConf{SYSConf: SYSConf{DebVersion: "caller-config"}}
	callerPaths := testerPathState{
		rootPath:                   "caller-root",
		defaultLogPath:             "caller-default-log",
		logPath:                    "caller-log",
		logFile:                    "caller.log",
		configPath:                 "caller-etc",
		configFile:                 "caller.json",
		envFilePath:                "caller-env",
		envMainFile:                "caller.env",
		whitelistConfigFile:        "caller.whitelist",
		defaultWhitelistConfigFile: "caller.default.whitelist",
		blacklistConfigFile:        "caller.blacklist",
		defaultBlacklistConfigFile: "caller.default.blacklist",
		reqUserAgent:               "caller-agent",
		nodeInfoFile:               "caller-node.json",
		nodeInfoBackupFile:         "caller-node.backup",
	}
	_, whitelistNet, _ := net.ParseCIDR("192.0.2.0/24")
	_, blacklistNet, _ := net.ParseCIDR("198.51.100.0/24")
	callerWhitelist := map[*net.IPNet]int64{whitelistNet: 7}
	callerBlacklist := map[*net.IPNet]int64{blacklistNet: 9}
	callerExtraEnvFiles := []string{"caller-extra.env"}
	callerEnvFileKeys := map[string]struct{}{"CALLER_ENV": {}}

	mainConf.Store(callerConfig)
	ConfigInitialized = true
	restoreTesterPathState(callerPaths)
	Whitelist, Blacklist = callerWhitelist, callerBlacklist
	extraEnvFiles, envFileKeys = callerExtraEnvFiles, callerEnvFileKeys
	mustSetTestEnvironment(t, ownedValueEnvName, "before")
	mustSetTestEnvironment(t, ownedEmptyEnvName, "")
	_ = os.Unsetenv(ownedAbsentEnvName)

	var previousTempRoot string
	for range 2 {
		mustSetTestEnvironment(t, unrelatedEnvName, "before")
		InitTester()
		t.Cleanup(StopTester)
		if Config() == nil || !ConfigInitialized {
			t.Fatal("tester did not initialize config")
		}
		if Config() == callerConfig || Config().LogConf.PostInterval != 7 {
			t.Fatal("tester did not publish isolated test config")
		}

		tempRoot := filepath.Dir(RootPath)
		if tempRoot == previousTempRoot {
			t.Fatal("tester reused temporary root across lifecycles")
		}
		previousTempRoot = tempRoot
		assertTesterPathLayout(t, tempRoot)
		for _, dir := range []string{tempRoot, DefaultLogPath, ConfigPath} {
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				t.Fatalf("tester directory %q is unavailable: %v", dir, err)
			}
		}
		if len(Whitelist) != 0 || len(Blacklist) != 0 {
			t.Fatal("tester did not isolate IP list state")
		}
		if !slices.Equal(extraEnvFiles, []string{EnvMainFile}) || len(envFileKeys) != 0 {
			t.Fatal("tester did not isolate env file tracking state")
		}
		for _, key := range []string{ownedValueEnvName, ownedEmptyEnvName, ownedAbsentEnvName} {
			assertTestEnvironment(t, key, "", false)
		}

		mustSetTestEnvironment(t, ownedValueEnvName, "during")
		mustSetTestEnvironment(t, ownedEmptyEnvName, "during")
		mustSetTestEnvironment(t, ownedAbsentEnvName, "during")
		mustSetTestEnvironment(t, unrelatedEnvName, "during")

		StopTester()
		if Config() != callerConfig {
			t.Fatal("tester did not restore main config pointer")
		}
		if !ConfigInitialized || captureTesterPathState() != callerPaths {
			t.Fatal("tester did not restore config paths")
		}
		if !maps.Equal(Whitelist, callerWhitelist) || !maps.Equal(Blacklist, callerBlacklist) {
			t.Fatal("tester did not restore IP list state")
		}
		if !slices.Equal(extraEnvFiles, callerExtraEnvFiles) || !maps.Equal(envFileKeys, callerEnvFileKeys) {
			t.Fatal("tester did not restore env file tracking state")
		}
		assertTestEnvironment(t, ownedValueEnvName, "before", true)
		assertTestEnvironment(t, ownedEmptyEnvName, "", true)
		assertTestEnvironment(t, ownedAbsentEnvName, "", false)
		assertTestEnvironment(t, unrelatedEnvName, "during", true)
		if _, err := os.Stat(tempRoot); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("temporary root still exists after StopTester: %v", err)
		}
	}
}

// TestTesterRejectsNestedInit 冻结助手不支持嵌套调用的边界.
func TestTesterRejectsNestedInit(t *testing.T) {
	InitTester()
	t.Cleanup(StopTester)

	defer func() {
		if recover() == nil {
			t.Fatal("nested InitTester should panic")
		}
	}()
	InitTester()
}

// testerPathState 汇总测试助手触达的路径和路径派生值.
type testerPathState struct {
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
	nodeInfoFile               string
	nodeInfoBackupFile         string
}

// captureTesterPathState 获取当前路径状态, 用于精确验证助手恢复边界.
func captureTesterPathState() testerPathState {
	return testerPathState{
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
		nodeInfoFile:               NodeInfoFile,
		nodeInfoBackupFile:         NodeInfoBackupFile,
	}
}

// restoreTesterPathState 恢复测试夹具主动改写的路径状态.
func restoreTesterPathState(state testerPathState) {
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
	NodeInfoFile = state.nodeInfoFile
	NodeInfoBackupFile = state.nodeInfoBackupFile
}

// assertTesterPathLayout 验证助手临时目录及全部派生路径的精确关系.
func assertTesterPathLayout(t *testing.T, tempRoot string) {
	t.Helper()
	want := testerPathState{
		rootPath:                   filepath.Join(tempRoot, "bin"),
		defaultLogPath:             filepath.Join(tempRoot, "log"),
		logPath:                    filepath.Join(tempRoot, "log"),
		logFile:                    filepath.Join(tempRoot, "log", BinName+".log"),
		configPath:                 filepath.Join(tempRoot, "etc"),
		configFile:                 filepath.Join(tempRoot, "etc", BinName+".json"),
		envFilePath:                filepath.Join(tempRoot, "env"),
		envMainFile:                filepath.Join(tempRoot, "env", BinName+".env"),
		whitelistConfigFile:        filepath.Join(tempRoot, "etc", BinName+".whitelist.conf"),
		defaultWhitelistConfigFile: filepath.Join(tempRoot, "etc", BinName+".whitelist.conf"),
		blacklistConfigFile:        filepath.Join(tempRoot, "etc", BinName+".blacklist.conf"),
		defaultBlacklistConfigFile: filepath.Join(tempRoot, "etc", BinName+".blacklist.conf"),
		reqUserAgent:               AppName + "/" + Version,
		nodeInfoBackupFile:         filepath.Join(tempRoot, "etc", "node_info.backup"),
	}
	if got := captureTesterPathState(); got != want {
		t.Fatalf("tester path layout = %#v, want %#v", got, want)
	}
}

// preserveTestEnvironment 在测试结束时恢复指定环境键的原始值和存在性.
func preserveTestEnvironment(t *testing.T, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}

// mustSetTestEnvironment 设置测试环境变量, 失败时保留错误上下文.
func mustSetTestEnvironment(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set test environment %q: %v", key, err)
	}
}

// assertTestEnvironment 同时验证环境变量的存在性和值.
func assertTestEnvironment(t *testing.T, key, want string, wantExists bool) {
	t.Helper()
	got, exists := os.LookupEnv(key)
	if got != want || exists != wantExists {
		t.Fatalf("environment %q = %q/%t, want %q/%t", key, got, exists, want, wantExists)
	}
}
