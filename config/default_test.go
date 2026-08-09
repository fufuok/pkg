package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fufuok/pkg/assert"
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
	preserveConfigPackageState(t)
	clearTestEnvironment(t, MainConfigNameEnvName)

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
	preserveConfigPackageState(t)
	clearTestEnvironment(t, MainConfigFileEnvName)
	clearTestEnvironment(t, MainConfigNameEnvName)

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
	preserveConfigPackageState(t)
	clearTestEnvironment(t, MainConfigFileEnvName)
	clearTestEnvironment(t, MainConfigNameEnvName)

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

// TestLoadEnvFilesClearsRemovedVars 验证 env 文件中注释/删除的变量在热加载后被置空.
// godotenv.Overload 只设置文件中存在的 key, 注释掉的 key 不会出现在解析结果中;
// loadEnvFiles 通过对比上次记录的 key 集合, 将已移除的 key 在进程环境中置空.
func TestLoadEnvFilesClearsRemovedVars(t *testing.T) {
	preserveConfigPackageState(t)

	// 保存并恢复 envFileKeys, 避免影响其他测试.
	oldEnvFileKeys := envFileKeys
	oldExtraEnvFiles := extraEnvFiles
	t.Cleanup(func() {
		envFileKeys = oldEnvFileKeys
		extraEnvFiles = oldExtraEnvFiles
	})

	envDir := t.TempDir()
	EnvFilePath = envDir
	EnvMainFile = filepath.Join(envDir, "main.env")

	// 第一次加载: FF_ON=1, FF_REMOVE=2
	assert.Nil(t, os.WriteFile(EnvMainFile, []byte("FF_ON=1\nFF_REMOVE=2\n"), 0o600))
	clearTestEnvironment(t, "FF_ON")
	clearTestEnvironment(t, "FF_REMOVE")
	loadEnvFiles()
	assert.Equal(t, "1", os.Getenv("FF_ON"))
	assert.Equal(t, "2", os.Getenv("FF_REMOVE"))

	// 第二次加载: FF_REMOVE 被注释掉, FF_ON 保留, 新增 FF_NEW=3
	assert.Nil(t, os.WriteFile(EnvMainFile, []byte("FF_ON=1\n#FF_REMOVE=2\nFF_NEW=3\n"), 0o600))
	clearTestEnvironment(t, "FF_NEW")
	loadEnvFiles()
	assert.Equal(t, "1", os.Getenv("FF_ON"))
	assert.Equal(t, "", os.Getenv("FF_REMOVE"), "commented-out var should be cleared on reload")
	assert.Equal(t, "3", os.Getenv("FF_NEW"))
}
