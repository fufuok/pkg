package config

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/xcrypto"
)

// prepareConfigBehaviorTest 建立不依赖生产目录和进程环境的配置解析基线.
// 调用方可以覆盖任意字段, cleanup 会由 preserveConfigPackageState 完整恢复.
func prepareConfigBehaviorTest(t *testing.T) string {
	t.Helper()
	preserveConfigPackageState(t)

	root := t.TempDir()
	EnvFilePath = filepath.Join(root, "env")
	ConfigPath = filepath.Join(root, "etc")
	if err := os.MkdirAll(EnvFilePath, 0o755); err != nil {
		t.Fatalf("create env directory: %v", err)
	}
	if err := os.MkdirAll(ConfigPath, 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	EnvMainFile = filepath.Join(EnvFilePath, "app.env")
	ConfigFile = filepath.Join(ConfigPath, "app.json")
	DefaultWhitelistConfigFile = filepath.Join(ConfigPath, "app.whitelist.conf")
	DefaultBlacklistConfigFile = filepath.Join(ConfigPath, "app.blacklist.conf")
	WhitelistConfigFile = ""
	BlacklistConfigFile = ""
	AppBaseSecretValue = "unit-test-secret"
	AppConfigBody = nil
	Debug = false
	mainConf.Store(nil)
	Whitelist = nil
	Blacklist = nil
	extraEnvFiles = nil
	envFileKeys = nil
	AlarmOn.Store(false)

	for _, key := range []string{
		MainConfRemoteAPIEnvName,
		WebCertFileEnvName,
		WebKeyFileEnvName,
		WebSignKeyEnvName,
		NodeInfoBackupEnabledEnvName,
	} {
		t.Setenv(key, "")
	}
	return root
}

// TestLoadConfigSources 验证 AppConfigBody 优先路径和真实配置文件路径具有等价派生行为.
func TestLoadConfigSources(t *testing.T) {
	configBody := []byte(`{
  "sys_conf": {"watcher_interval": "45s", "req_timeout": "2s"},
  "log_conf": {"level": 1},
  "whitelist": ["127.0.0.1"],
  "blacklist": ["10.0.0.0/8,5"]
}`)

	tests := []struct {
		name      string
		useMemory bool
	}{
		{name: "application config body", useMemory: true},
		{name: "configuration file", useMemory: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareConfigBehaviorTest(t)
			if tt.useMemory {
				AppConfigBody = configBody
				ConfigFile = filepath.Join(t.TempDir(), "missing.json")
			} else if err := os.WriteFile(ConfigFile, configBody, 0o600); err != nil {
				t.Fatalf("write config file: %v", err)
			}

			if err := LoadConfig(); err != nil {
				t.Fatalf("load config: %v", err)
			}
			cfg := Config()
			assert.True(t, cfg != nil)
			assert.Equal(t, 45*time.Second, cfg.SYSConf.WatcherIntervalDuration)
			assert.Equal(t, 2*time.Second, cfg.SYSConf.ReqTimeoutDuration)
			assert.Equal(t, ConfigFile, cfg.MainConf.Path)
			assert.Equal(t, "unit-test-secret", cfg.SYSConf.BaseSecretValue)
			assert.Equal(t, int64(0), mustLookupIPNet(t, "127.0.0.1", Whitelist))
			assert.Equal(t, int64(5), mustLookupIPNet(t, "10.2.3.4", Blacklist))
		})
	}
}

// TestLoadConfigKeepsPublishedPointerOnReadFailure 验证读取失败不会替换已发布配置指针.
// 解析过程中其他包级派生值是否原子回滚不属于一期契约, 本测试不对其做断言.
func TestLoadConfigKeepsPublishedPointerOnReadFailure(t *testing.T) {
	prepareConfigBehaviorTest(t)
	original := &MainConf{SYSConf: SYSConf{DebVersion: "published"}}
	mainConf.Store(original)
	AppConfigBody = []byte(`{"sys_conf":`)

	err := LoadConfig()
	assert.True(t, err != nil)
	assert.True(t, Config() == original)
}

// TestParseDurationMatrix 冻结空值、最小值回退、合法值和格式错误四类规则.
func TestParseDurationMatrix(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		defaultValue time.Duration
		minimum      time.Duration
		want         time.Duration
		wantErr      bool
	}{
		{name: "empty uses default", defaultValue: time.Minute, minimum: time.Second, want: time.Minute},
		{name: "below minimum uses default", value: "500ms", defaultValue: time.Minute, minimum: time.Second, want: time.Minute},
		{name: "minimum is accepted", value: "1s", defaultValue: time.Minute, minimum: time.Second, want: time.Second},
		{name: "valid duration", value: "3.5s", defaultValue: time.Minute, minimum: time.Second, want: 3500 * time.Millisecond},
		{name: "invalid duration", value: "later", defaultValue: time.Minute, minimum: time.Second, want: time.Minute, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.value, tt.defaultValue, tt.minimum)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}

// TestParseSYSConfigDurationContext 验证两类时长错误保留配置字段上下文.
func TestParseSYSConfigDurationContext(t *testing.T) {
	prepareConfigBehaviorTest(t)

	cfg := &MainConf{SYSConf: SYSConf{WatcherInterval: "bad"}}
	err := parseSYSConfig(cfg)
	assert.True(t, err != nil)
	assert.Contains(t, "parse watcher_interval err", err.Error())

	cfg = &MainConf{SYSConf: SYSConf{WatcherInterval: "30s", ReqTimeout: "bad", DebVersion: " 1.2.3;rm "}}
	err = parseSYSConfig(cfg)
	assert.True(t, err != nil)
	assert.Contains(t, "parse req_timeout err", err.Error())
	assert.Equal(t, "1.2.3;rm", cfg.SYSConf.DebVersion)
}

// TestParseLogAndAlarmConfig 验证日志默认派生、环境变量覆盖和报警开关.
func TestParseLogAndAlarmConfig(t *testing.T) {
	prepareConfigBehaviorTest(t)
	LogFile = filepath.Join(t.TempDir(), "app.log")
	LogSamplePeriodDur = 3 * time.Second
	LogSampleBurst = 7
	LogPostIntervalDuration = 4 * time.Second
	LogPostBatchNum = 11
	LogPostBatchBytes = 13 << 10
	LogFileMaxSize = 17
	LogFileMaxBackups = 19
	LogFileMaxAge = 23

	t.Setenv("PKG_TEST_POST_API", " https://logs.local ")
	t.Setenv("PKG_TEST_ALARM_API", " https://alarm.local ")
	t.Setenv("PKG_TEST_ALARM_CODE", " TEST-CODE ")
	cfg := &MainConf{LogConf: LogConf{
		Level:           99,
		PostAPIEnv:      "PKG_TEST_POST_API",
		PostAlarmAPIEnv: "PKG_TEST_ALARM_API",
		AlarmCodeEnv:    "PKG_TEST_ALARM_CODE",
	}}

	parseLogConfig(cfg)
	parseAlarmOnConfig(cfg)
	assert.Equal(t, LogLevel, cfg.LogConf.Level)
	assert.Equal(t, 3*time.Second, cfg.LogConf.PeriodDuration)
	assert.Equal(t, uint32(7), cfg.LogConf.Burst)
	assert.Equal(t, 4*time.Second, cfg.LogConf.PostIntervalDuration)
	assert.Equal(t, 11, cfg.LogConf.PostBatchNum)
	assert.Equal(t, 13<<10, cfg.LogConf.PostBatchBytes)
	assert.Equal(t, LogFile, cfg.LogConf.File)
	assert.Equal(t, int64(17), cfg.LogConf.MaxSize)
	assert.Equal(t, 19, cfg.LogConf.MaxBackups)
	assert.Equal(t, 23, cfg.LogConf.MaxAge)
	assert.Equal(t, "https://logs.local", cfg.LogConf.PostAPI)
	assert.Equal(t, "https://alarm.local", cfg.LogConf.PostAlarmAPI)
	assert.Equal(t, "TEST-CODE", cfg.LogConf.AlarmCode)
	assert.True(t, AlarmOn.Load())
	assert.Equal(t, "", os.Getenv("PKG_TEST_POST_API"))

	cfg.LogConf = LogConf{AlarmCode: AlarmDisabledValue, PostAlarmAPI: "https://alarm.local", Period: 2, Burst: 3, PostInterval: 5, PostBatchNum: 6, PostBatchMB: 2}
	parseLogConfig(cfg)
	parseAlarmOnConfig(cfg)
	assert.Equal(t, 2*time.Second, cfg.LogConf.PeriodDuration)
	assert.Equal(t, 5*time.Second, cfg.LogConf.PostIntervalDuration)
	assert.Equal(t, 2<<20, cfg.LogConf.PostBatchBytes)
	assert.Equal(t, "", cfg.LogConf.AlarmCode)
	assert.False(t, AlarmOn.Load())
}

// TestParseRemoteFileConfigMatrix 验证远端文件密钥、周期、随机等待和路径归一化.
func TestParseRemoteFileConfigMatrix(t *testing.T) {
	prepareConfigBehaviorTest(t)
	const secret = "remote-secret"
	const secretEnv = "PKG_TEST_REMOTE_SECRET"
	clearTestEnvironment(t, secretEnv)
	if _, err := xcrypto.SetenvEncrypt(secretEnv, "token-value", secret); err != nil {
		t.Fatalf("encrypt remote secret: %v", err)
	}

	cfg := FilesConf{Path: "  local.conf  ", Method: "GetDataSource", SecretName: secretEnv, Interval: 30, RandomWait: 9}
	if err := ParseRemoteFileConfig(&cfg, secret); err != nil {
		t.Fatalf("parse remote file config: %v", err)
	}
	assert.Equal(t, "local.conf", cfg.Path)
	assert.Equal(t, "GetDataSource", cfg.Method)
	assert.Equal(t, "token-value", cfg.SecretValue)
	assert.Equal(t, 30*time.Second, cfg.GetConfDuration)
	assert.Equal(t, 9, cfg.RandomWait)

	missing := FilesConf{SecretName: "PKG_TEST_MISSING_SECRET", Interval: 29}
	t.Setenv(missing.SecretName, "")
	err := ParseRemoteFileConfig(&missing, secret)
	assert.True(t, err != nil)
	assert.Contains(t, missing.SecretName+" cannot be empty", err.Error())

	disabled := FilesConf{Interval: 29}
	assert.Nil(t, ParseRemoteFileConfig(&disabled, secret))
	assert.Equal(t, time.Duration(0), disabled.GetConfDuration)
	assert.Equal(t, DefaultRandomWait, disabled.RandomWait)
}

// TestParseMainRemoteConfigOverridesAPI 验证主配置路径固定为本地主配置且 env API 优先.
func TestParseMainRemoteConfigOverridesAPI(t *testing.T) {
	prepareConfigBehaviorTest(t)
	t.Setenv(MainConfRemoteAPIEnvName, "https://config.local/?token=")
	cfg := &MainConf{SYSConf: SYSConf{BaseSecretValue: "secret"}, MainConf: FilesConf{Path: "ignored", API: "https://file.local", Interval: 30}}

	if err := parseMainRemoteConfig(cfg); err != nil {
		t.Fatalf("parse main remote config: %v", err)
	}
	assert.Equal(t, ConfigFile, cfg.MainConf.Path)
	assert.Equal(t, "https://config.local/?token=", cfg.MainConf.API)
	assert.Equal(t, 30*time.Second, cfg.MainConf.GetConfDuration)
	assert.Equal(t, "", os.Getenv(MainConfRemoteAPIEnvName))
}

// TestParseIPListFiles 验证内联名单与文件名单合并, 并保留非法 CIDR 上下文.
func TestParseIPListFiles(t *testing.T) {
	root := prepareConfigBehaviorTest(t)
	whitelistFile := filepath.Join(root, "whitelist.conf")
	blacklistFile := filepath.Join(root, "blacklist.conf")
	assert.Nil(t, os.WriteFile(whitelistFile, []byte("10.0.0.0/8,2\n"), 0o600))
	assert.Nil(t, os.WriteFile(blacklistFile, []byte("192.168.0.0/16,3\n"), 0o600))
	cfg := &MainConf{
		SYSConf:       SYSConf{BaseSecretValue: "secret"},
		Whitelist:     []string{"127.0.0.1,1"},
		Blacklist:     []string{"::1,4"},
		WhitelistConf: FilesConf{Path: whitelistFile},
		BlacklistConf: FilesConf{Path: blacklistFile},
	}

	assert.Nil(t, parseWhitelistConfig(cfg))
	assert.Nil(t, parseBlacklistConfig(cfg))
	assert.Equal(t, whitelistFile, WhitelistConfigFile)
	assert.Equal(t, blacklistFile, BlacklistConfigFile)
	assert.Equal(t, int64(1), mustLookupIPNet(t, "127.0.0.1", Whitelist))
	assert.Equal(t, int64(2), mustLookupIPNet(t, "10.1.2.3", Whitelist))
	assert.Equal(t, int64(3), mustLookupIPNet(t, "192.168.1.1", Blacklist))
	assert.Equal(t, int64(4), mustLookupIPNet(t, "::1", Blacklist))

	cfg.Whitelist = []string{"not-an-ip"}
	cfg.WhitelistConf.Path = filepath.Join(root, "missing.conf")
	err := parseWhitelistConfig(cfg)
	assert.True(t, err != nil)
	assert.Contains(t, "not-an-ip", err.Error())
}

// TestGetEnvFilesReturnsSnapshot 验证调用方不能通过返回切片修改 env 文件注册表.
func TestGetEnvFilesReturnsSnapshot(t *testing.T) {
	prepareConfigBehaviorTest(t)
	extraEnvFiles = []string{"one.env", "two.env"}
	got := GetEnvFiles()
	got[0] = "changed.env"
	assert.Equal(t, "one.env", extraEnvFiles[0])
}

// TestLoadEnvFilesOverrideOrder 验证主 env 先加载、额外 env 后加载并覆盖同名键.
func TestLoadEnvFilesOverrideOrder(t *testing.T) {
	prepareConfigBehaviorTest(t)
	extraFile := filepath.Join(EnvFilePath, "extra.env")
	assert.Nil(t, os.WriteFile(EnvMainFile, []byte("PKG_ENV_ORDER=main\nPKG_ENV_MAIN_ONLY=main\n"), 0o600))
	assert.Nil(t, os.WriteFile(extraFile, []byte("PKG_ENV_ORDER=extra\nPKG_ENV_EXTRA_ONLY=extra\n"), 0o600))
	for _, key := range []string{"PKG_ENV_ORDER", "PKG_ENV_MAIN_ONLY", "PKG_ENV_EXTRA_ONLY"} {
		clearTestEnvironment(t, key)
	}

	loadEnvFiles("extra.env")
	assert.Equal(t, "extra", os.Getenv("PKG_ENV_ORDER"))
	assert.Equal(t, "main", os.Getenv("PKG_ENV_MAIN_ONLY"))
	assert.Equal(t, "extra", os.Getenv("PKG_ENV_EXTRA_ONLY"))
	files := GetEnvFiles()
	assert.Equal(t, EnvMainFile, files[0])
	assert.Equal(t, extraFile, files[1])
}

// mustLookupIPNet 返回名单值, 找不到时立即终止当前测试.
func mustLookupIPNet(t *testing.T, ip string, networks map[*net.IPNet]int64) int64 {
	t.Helper()
	value, ok := lookupIPNetsString(ip, networks)
	if !ok {
		t.Fatalf("IP %s not found in network list", ip)
	}
	return value
}
