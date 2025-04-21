package config

import (
	"time"

	"github.com/fufuok/utils"
)

var (
	// DefaultTimeZone 默认时区名称, 固定为: UTC+8
	DefaultTimeZone = utils.ChinaTimeZone

	// DefaultTimeLocation 默认时区, 固定为中国时区
	DefaultTimeLocation *time.Location

	// ZeroTimeCST 中国时区 0 值
	ZeroTimeCST time.Time
	ZeroTimeUTC time.Time
)

func init() {
	DefaultTimeLocation = utils.InitChinaLocation()
	ZeroTimeCST = time.Date(1, 1, 1, 0, 0, 0, 0, DefaultTimeLocation)
	ZeroTimeUTC = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
}
