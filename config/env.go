package config

import (
	"os"
	"path/filepath"

	"github.com/fufuok/utils/base58"
	"github.com/joho/godotenv"
)

var (
	ExtraEnvFiles []string
)

// 读取 .env 配置
// 后加载的优先, 已存在的环境变量值会被覆盖
// 忽略不存在的文件
func loadEnvFiles(filenames ...string) error {
	_ = godotenv.Overload(EnvMainFile)
	ExtraEnvFiles = []string{}
	for _, f := range filenames {
		if !filepath.IsAbs(f) {
			f = filepath.Join(EnvFilePath, f)
		}
		if f == EnvMainFile {
			continue
		}
		_ = godotenv.Overload(f)
		// 放入待监视文件列表
		ExtraEnvFiles = append(ExtraEnvFiles, f)
	}
	setDefaultConfig()
	return nil
}

// 加载环境变量中设定的应用配置
func setDefaultConfig() {
	if s := os.Getenv("BASE_SECRET_KEY_NAME"); s != "" {
		BaseSecretKeyName = s
	}
	// 盐是 Base58 编码后存入 env, 缺省为固定值+AppName
	if s := string(base58.Decode(os.Getenv("BASE_SECRET_SALT"))); s != "" {
		BaseSecretSalt = s
	}
}
