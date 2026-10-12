package conf

import (
	"path/filepath"

	"github.com/fufuok/pkg/config"
)

const (
	// DefaultHeartbeatSpec 每 2 秒执行一次, 用于观察 crontab 和退出取消.
	DefaultHeartbeatSpec = "*/2 * * * * *"

	// DataConfTopic 是应用配置发布后的通知主题.
	DataConfTopic = "DataConf"
)

// DataRootPath 是数据目录, 对应部署布局中的 data/.
var DataRootPath string

// initDefaultConfig 派生应用数据目录.
func initDefaultConfig() {
	// 不创建目录, 也不覆盖调用方预先设置的值.
	if DataRootPath == "" {
		DataRootPath = filepath.Join(config.RootPath, "..", "data")
	}
}
