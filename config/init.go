package config

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fufuok/utils/xcrypto"
	"github.com/fufuok/utils/xfile"
)

type M struct{}

// Start 程序启动时初始化
func (m *M) Start() error {
	initDefaultConfig()
	if err := LoadConfig(); err != nil {
		log.Fatalln("Failed to initialize main config:", err, "\nbye.")
	}
	return nil
}

// Runtime 重新加载配置时运行
func (m *M) Runtime() error {
	return nil
}

// Stop 程序退出时运行
func (m *M) Stop() error {
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

// ParseDuration 解析时间间隔字符串
func ParseDuration(s string, defDur time.Duration, minDur ...time.Duration) (time.Duration, error) {
	if s == "" {
		return defDur, nil
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return defDur, err
	}

	if len(minDur) > 0 && dur < minDur[0] {
		return defDur, nil
	}
	return dur, nil
}

// ParseRemoteFileConfig 解析远端配置获取配置项
func ParseRemoteFileConfig(cfg *FilesConf, secret string) error {
	// 远程获取配置 API, 解密 SecretName
	if cfg.SecretName != "" {
		cfg.SecretValue = xcrypto.GetenvDecrypt(cfg.SecretName, secret)
		if cfg.SecretValue == "" {
			return fmt.Errorf("%s cannot be empty", cfg.SecretName)
		}
	}
	// 每次获取远程配置的时间间隔, < 30 秒则禁用该功能
	if cfg.Interval >= 30 {
		cfg.GetConfDuration = time.Duration(cfg.Interval) * time.Second
	}
	// 配置拉取执行前最大随机等待秒数
	if cfg.RandomWait <= 0 {
		cfg.RandomWait = DefaultRandomWait
	}
	cfg.Path = strings.TrimSpace(cfg.Path)
	return nil
}
