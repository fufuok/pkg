package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fufuok/utils/assert"
)

// TestResolveMainConfigFile 覆盖启动级主配置选择的纯函数规则.
// 这些用例只读环境变量并返回路径, 不依赖全局目录创建, 用来锁定
// 显式 ConfigFile 之外的优先级: MAIN_CONFIG_FILE > MAIN_CONFIG_NAME > 默认文件.
func TestResolveMainConfigFile(t *testing.T) {
	configPath := filepath.Join("opt", "app", "etc")
	absFile := filepath.Join(t.TempDir(), "main.json")

	tests := []struct {
		name       string
		fileEnv    string
		nameEnv    string
		configPath string
		binName    string
		want       string
	}{
		{
			name:       "default config file",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "xydatarouter.json"),
		},
		{
			name:       "config name appends json suffix",
			nameEnv:    "xydatarouter.lagz",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "xydatarouter.lagz.json"),
		},
		{
			name:       "config name keeps existing json suffix case insensitive",
			nameEnv:    "xydatarouter.lagz.JSON",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "xydatarouter.lagz.JSON"),
		},
		{
			name:       "relative config file uses config path",
			fileEnv:    "xydatarouter.gray.json",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "xydatarouter.gray.json"),
		},
		{
			name:       "absolute config file is used directly",
			fileEnv:    absFile,
			nameEnv:    "xydatarouter.lagz",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       absFile,
		},
		{
			name:       "main config file wins over main config name",
			fileEnv:    "xydatarouter.gray.json",
			nameEnv:    "xydatarouter.lagz",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "xydatarouter.gray.json"),
		},
		{
			name:       "config name trims whitespace",
			nameEnv:    "  local-xydatarouter  ",
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "local-xydatarouter.json"),
		},
		{
			name:       "config name is reduced to base name",
			nameEnv:    filepath.Join("..", "..", "etc", "passwd"),
			configPath: configPath,
			binName:    "xydatarouter",
			want:       filepath.Join(configPath, "passwd.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(MainConfigFileEnvName, tt.fileEnv)
			t.Setenv(MainConfigNameEnvName, tt.nameEnv)

			got := resolveMainConfigFile(tt.configPath, tt.binName)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveDefaultConfigFile 覆盖通用路径型配置解析规则.
// 该函数面向应用侧旁路配置, 环境变量值表示完整文件路径; 显式路径缺失不在这里兜底,
// 由调用方加载配置时返回错误, 以便尽早暴露部署问题.
func TestResolveDefaultConfigFile(t *testing.T) {
	configPath := filepath.Join("opt", "app", "etc")
	absFile := filepath.Join(t.TempDir(), "api.json")

	tests := []struct {
		name    string
		envFile string
		want    string
	}{
		{
			name: "empty env uses bin name suffix",
			want: filepath.Join(configPath, "xydatarouter.api.json"),
		},
		{
			name:    "blank env uses bin name suffix",
			envFile: "  ",
			want:    filepath.Join(configPath, "xydatarouter.api.json"),
		},
		{
			name:    "relative file uses config path",
			envFile: "xydatarouter.api.lagz.json",
			want:    filepath.Join(configPath, "xydatarouter.api.lagz.json"),
		},
		{
			name:    "absolute file is used directly",
			envFile: absFile,
			want:    absFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_CONFIG_FILE", tt.envFile)
			got := resolveDefaultConfigFile(configPath, "xydatarouter", "APP_CONFIG_FILE", ".api.json")
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveDefaultConfigName 覆盖通用名称型配置解析规则.
// 名称型环境变量只允许选择 ConfigPath 下的文件名, 会取 base 名并补齐后缀,
// 适合 MAIN_CONFIG_NAME 这类不希望接受任意路径的配置入口.
func TestResolveDefaultConfigName(t *testing.T) {
	configPath := filepath.Join("opt", "app", "etc")

	tests := []struct {
		name    string
		envName string
		want    string
	}{
		{
			name: "empty env uses bin name suffix",
			want: filepath.Join(configPath, "xydatarouter.json"),
		},
		{
			name:    "blank env uses bin name suffix",
			envName: "  ",
			want:    filepath.Join(configPath, "xydatarouter.json"),
		},
		{
			name:    "name appends suffix",
			envName: "xydatarouter.lagz",
			want:    filepath.Join(configPath, "xydatarouter.lagz.json"),
		},
		{
			name:    "name keeps suffix case insensitive",
			envName: "xydatarouter.lagz.JSON",
			want:    filepath.Join(configPath, "xydatarouter.lagz.JSON"),
		},
		{
			name:    "name is reduced to base name",
			envName: filepath.Join("..", "..", "etc", "worker"),
			want:    filepath.Join(configPath, "worker.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_CONFIG_NAME", tt.envName)
			got := resolveDefaultConfigName(configPath, "xydatarouter", "APP_CONFIG_NAME", ".json")
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestLoadBootstrapEnvKeepsMachineEnv 验证启动级 .default.env 只提供默认值.
// 当机器环境变量已经存在时, godotenv.Load 不会用文件值覆盖它, 以便 systemd,
// /etc/default, 容器 env 或 shell env 能按机器维度覆盖随包分发的默认值.
func TestLoadBootstrapEnvKeepsMachineEnv(t *testing.T) {
	restoreDefaultConfigGlobals(t)
	restoreEnvVar(t, MainConfigNameEnvName)

	BinName = "xydatarouter"
	EnvFilePath = t.TempDir()
	assert.Nil(t, os.WriteFile(filepath.Join(EnvFilePath, BinName+BootstrapEnvSuffix), []byte("MAIN_CONFIG_NAME=fromfile\n"), 0o600))
	assert.Nil(t, os.Setenv(MainConfigNameEnvName, "fromenv"))

	loadBootstrapEnv()

	assert.Equal(t, "fromenv", os.Getenv(MainConfigNameEnvName))
}

// TestInitDefaultConfigLoadsBootstrapBeforeConfigFile 验证 initDefaultConfig 的真实调用顺序.
// EnvFilePath 必须先于 ConfigFile 派生, 否则启动级 env/{BinName}.default.env 无法在
// ConfigFile 解析前注入 MAIN_CONFIG_NAME, 该测试会回退到默认 BinName.json 而失败.
func TestInitDefaultConfigLoadsBootstrapBeforeConfigFile(t *testing.T) {
	restoreDefaultConfigGlobals(t)
	restoreEnvVar(t, MainConfigFileEnvName)
	restoreEnvVar(t, MainConfigNameEnvName)

	root := filepath.Join(t.TempDir(), "bin")
	envDir := filepath.Join(root, "..", "env")
	assert.Nil(t, os.MkdirAll(envDir, 0o755))
	assert.Nil(t, os.WriteFile(filepath.Join(envDir, "xydatarouter"+BootstrapEnvSuffix), []byte("MAIN_CONFIG_NAME=fromfile\n"), 0o600))

	BinName = "xydatarouter"
	RootPath = root

	initDefaultConfig()

	assert.Equal(t, filepath.Join(root, "..", "etc", "fromfile.json"), ConfigFile)
	assert.Equal(t, filepath.Join(root, "..", "env"), EnvFilePath)
	assert.Equal(t, filepath.Join(root, "..", "env", "xydatarouter.env"), EnvMainFile)
}

// TestInitDefaultConfigKeepsExplicitConfigFile 验证应用 init 或命令行 -c 预先写入
// ConfigFile 后, 启动级 MAIN_CONFIG_FILE/MAIN_CONFIG_NAME 不会改变该显式选择.
func TestInitDefaultConfigKeepsExplicitConfigFile(t *testing.T) {
	restoreDefaultConfigGlobals(t)
	restoreEnvVar(t, MainConfigFileEnvName)
	restoreEnvVar(t, MainConfigNameEnvName)

	root := filepath.Join(t.TempDir(), "bin")
	explicitFile := filepath.Join(t.TempDir(), "manual.json")
	BinName = "xydatarouter"
	RootPath = root
	ConfigFile = explicitFile
	assert.Nil(t, os.Setenv(MainConfigFileEnvName, filepath.Join(t.TempDir(), "ignored.json")))
	assert.Nil(t, os.Setenv(MainConfigNameEnvName, "ignored"))

	initDefaultConfig()

	assert.Equal(t, explicitFile, ConfigFile)
}

// restoreDefaultConfigGlobals 保存并恢复 initDefaultConfig 会读写的包级变量.
// config 包大量默认值是进程级全局状态, 单测必须显式恢复, 避免影响后续测试.
func restoreDefaultConfigGlobals(t *testing.T) {
	t.Helper()

	oldBinName := BinName
	oldDefaultLogPath := DefaultLogPath
	oldRootPath := RootPath
	oldLogPath := LogPath
	oldLogFile := LogFile
	oldConfigPath := ConfigPath
	oldConfigFile := ConfigFile
	oldEnvFilePath := EnvFilePath
	oldEnvMainFile := EnvMainFile
	oldNodeInfoBackupFile := NodeInfoBackupFile
	oldReqUserAgent := ReqUserAgent
	oldDefaultWhitelistConfigFile := DefaultWhitelistConfigFile
	oldDefaultBlacklistConfigFile := DefaultBlacklistConfigFile

	t.Cleanup(func() {
		BinName = oldBinName
		DefaultLogPath = oldDefaultLogPath
		RootPath = oldRootPath
		LogPath = oldLogPath
		LogFile = oldLogFile
		ConfigPath = oldConfigPath
		ConfigFile = oldConfigFile
		EnvFilePath = oldEnvFilePath
		EnvMainFile = oldEnvMainFile
		NodeInfoBackupFile = oldNodeInfoBackupFile
		ReqUserAgent = oldReqUserAgent
		DefaultWhitelistConfigFile = oldDefaultWhitelistConfigFile
		DefaultBlacklistConfigFile = oldDefaultBlacklistConfigFile
	})
}

// restoreEnvVar 保存并恢复环境变量, 支持测试中先 unset 再让 godotenv.Load 注入默认值.
// t.Setenv 会把空字符串视为已存在变量, 不适合验证 Load 对“不存在变量”的加载语义.
func restoreEnvVar(t *testing.T, key string) {
	t.Helper()

	old, ok := os.LookupEnv(key)
	assert.Nil(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
