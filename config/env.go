package config

import (
	"os"
	"path/filepath"

	"github.com/fufuok/utils/base58"
	"github.com/joho/godotenv"
)

var ExtraEnvFiles []string

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
	ExtraEnvFiles = envFiles
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
}
