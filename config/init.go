package config

import (
	"log"

	"github.com/fufuok/utils/xfile"
)

// Start 程序启动时初始化配置
func Start() error {
	initDefaultConfig()
	if err := LoadConf(); err != nil {
		log.Fatalln("Failed to initialize main config:", err, "\nbye.")
	}
	return nil
}

// Runtime 重新加载配置时运行
func Runtime() error {
	return nil
}

func Stop() error {
	return nil
}

// Config 获取全局配置
func Config() *MainConf {
	return mainConf.Load()
}

// IsSkipRemoteConfig 是否只使用本地配置文件
func IsSkipRemoteConfig() bool {
	f := Config().SYSConf.SkipRemoteConfig
	return f != "" && xfile.IsFile(f)
}
