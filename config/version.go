package config

import (
	"strings"
)

var (
	Version   = "v0.0.1"
	GoVersion = ""
	GitCommit = ""

	// DebVersion 当前的包版本信息
	DebVersion = ""
)

// GetDevName 去除服务名称后缀, 得到包名
func GetDevName(name string) string {
	if strings.HasSuffix(name, ServiceNameSuffix) {
		return name[:len(name)-len(ServiceNameSuffix)]
	}
	return name
}
