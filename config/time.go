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

	// CSTTimeLocation 强制的中国时区
	CSTTimeLocation *time.Location

	// ZeroTimeCST 中国时区 0 值
	ZeroTimeCST time.Time
	ZeroTimeUTC time.Time
)

func init() {
	DefaultTimeZone, DefaultTimeLocation, CSTTimeLocation, _ = utils.InitCSTLocation()
	ZeroTimeCST = time.Date(1, 1, 1, 0, 0, 0, 0, CSTTimeLocation)
	ZeroTimeUTC = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
}
