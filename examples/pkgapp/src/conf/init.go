package conf

import (
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/pubsub"
)

// M 是应用配置模块.
type M struct{}

// Start 初始化并发布应用配置.
func (m *M) Start() error {
	initDefaultConfig()
	loadEnvConfig()
	Notifier = pubsub.New[string, struct{}](1)
	return LoadConfig()
}

// Runtime 重新加载应用配置.
func (m *M) Runtime() error {
	// pkg 只负责把 env 文件载入进程环境, 应用变量在这里刷新.
	loadEnvConfig()
	return LoadConfig()
}

// Stop 在业务模块停止后释放应用快照和通知器.
func (m *M) Stop() error {
	appMainConf.Store(nil)
	if Notifier != nil {
		Notifier.Shutdown()
		Notifier = nil
	}
	return nil
}

// Main 返回 pkg 公共配置.
func Main() *config.MainConf {
	return config.Config()
}

// App 返回当前应用配置.
func App() *AppMainConf {
	return appMainConf.Load()
}
