package config

import (
	"strings"
)

var (
	Version   = "0.0.1"
	GoVersion = ""
	GitCommit = ""

	// DebVersion 启动时查询的自包版本, 后台安装不写此变量, 避免与业务读取竞争.
	DebVersion = ""
)

// GetDevName 去除服务名称后缀, 得到包名
func GetDevName(name string) string {
	if strings.HasSuffix(name, ServiceNameSuffix) {
		return name[:len(name)-len(ServiceNameSuffix)]
	}
	return name
}
