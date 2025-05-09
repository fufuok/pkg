package config

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/fufuok/utils/base58"
	"github.com/joho/godotenv"
)

var extraEnvFiles []string

// GetEnvFiles 获取已加载的 .env 文件列表
func GetEnvFiles() []string {
	return slices.Clone(extraEnvFiles)
}

// 读取 .env 配置
// 后加载的优先, 已存在的环境变量值会被覆盖
// 忽略不存在的文件
// 注:
//
//	系统环境变量加载到程序环境后会一直存在, 重新加载会覆盖值,
//	但用户从系统环境变量中删除时, 不会影响到程序已加载的值.
//
// 如:
//
//	在系统中 env/local.env 有 export FF=666
//	程序启动后会得到 FF=666
//	用户将 env/local.env 中 export FF=666 这一行删除后, 程序中依然存在 FF=666
//	可以将变量置空而不是删除, 或重启程序
func loadEnvFiles(envFiles ...string) {
	envFiles = append([]string{EnvMainFile}, envFiles...)
	for i, f := range envFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(EnvFilePath, f)
			envFiles[i] = f
		}
		_ = godotenv.Overload(f)
	}
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
