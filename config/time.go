package config

import (
	"time"

	"github.com/fufuok/utils"
)

var (
	// DefaultTimeZone 缺省时区名称
	DefaultTimeZone string

	// DefaultTimeLocation 默认时区
	// 优先尝试解析中国时区 (GMT+8), 失败(Windows)后使用本地时区
	DefaultTimeLocation *time.Location
)

func init() {
	DefaultTimeZone, DefaultTimeLocation, _, _ = utils.InitCSTLocation()
}
