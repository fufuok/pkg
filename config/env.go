package config

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/fufuok/utils/base58"
	"github.com/joho/godotenv"
)

var extraEnvFiles []string

// envFileKeys 记录上次 loadEnvFiles 从 env 文件中解析到的全部 key.
// 用于热加载时检测 "上次存在但本次已从文件中移除 (注释/删除)" 的变量,
// 将其从进程环境中置空, 使应用读取 os.Getenv 时得到空值而非旧值.
var envFileKeys map[string]struct{}

// GetEnvFiles 获取已加载的 .env 文件列表
func GetEnvFiles() []string {
	return slices.Clone(extraEnvFiles)
}

// 读取 .env 配置
// 后加载的优先, 已存在的环境变量值会被覆盖.
// 忽略不存在的文件.
//
// 热加载语义: 当变量从 env 文件中被删除或注释掉时, 进程环境中对应的旧值会被置空,
// 使 os.Getenv 返回空字符串. 这解决了 godotenv.Overload 只设置文件中存在的 key,
// 注释/删除的 key 仍保留旧值的问题. 仅影响由 env 文件管理过的 key, 不影响系统级环境变量.
func loadEnvFiles(envFiles ...string) {
	envFiles = append([]string{EnvMainFile}, envFiles...)
	for i, f := range envFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(EnvFilePath, f)
			envFiles[i] = f
		}
	}

	// 先解析所有 env 文件内容, 收集本次文件中存在的 key 集合.
	// 使用 godotenv.Read 而非 Overload, 因为 Read 只解析不写入进程环境,
	// 且注释行不会出现在结果中 — 这正是检测 "变量已从文件移除" 的依据.
	currentKeys := make(map[string]struct{})
	for _, f := range envFiles {
		m, err := godotenv.Read(f)
		if err != nil {
			continue
		}
		for k := range m {
			currentKeys[k] = struct{}{}
		}
	}

	// 对比上次记录的 key: 上次有但本次没有的, 说明已从 env 文件中移除,
	// 将进程环境中的旧值置空. envFileKeys 为 nil (首次加载) 时跳过.
	for k := range envFileKeys {
		if _, ok := currentKeys[k]; !ok {
			_ = os.Setenv(k, "")
		}
	}

	// Overload 将文件中存在的 key 写入进程环境 (后加载的文件优先覆盖).
	for _, f := range envFiles {
		_ = godotenv.Overload(f)
	}

	envFileKeys = currentKeys
	extraEnvFiles = envFiles
	loadEnvConfig()
}

// 加载环境变量中设定的应用配置
func loadEnvConfig() {
	if s := os.Getenv(BaseSecretKeyNameEnvName); s != "" {
		_ = os.Setenv(BaseSecretKeyNameEnvName, "")
		BaseSecretEnvName = s
	}
	// 盐是 Base58 编码后存入 env, 缺省为固定值 (BaseSecretSalt 变量)
	if s := string(base58.Decode(os.Getenv(BaseSecretSaltEnvName))); s != "" {
		_ = os.Setenv(BaseSecretSaltEnvName, "")
		BaseSecretSalt = s
	}

	if s := os.Getenv(BinNameEnvName); s != "" {
		_ = os.Setenv(BinNameEnvName, "")
		BinName = s
	}
	if s := os.Getenv(AppNameEnvName); s != "" {
		_ = os.Setenv(AppNameEnvName, "")
		AppName = s
	}
	if s := os.Getenv(DebNameEnvName); s != "" {
		_ = os.Setenv(DebNameEnvName, "")
		DebName = s
	}

	// 应用指定的基础密钥解密 KEY 优先, 默认使用: 盐+AppName
	if BaseSecretKeyValue == "" {
		BaseSecretKeyValue = BaseSecretSalt + AppName
	}
}
