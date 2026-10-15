package config

import (
	"time"

	"github.com/fufuok/pkg/utils"
)

var (
	// DefaultTimeZone 默认时区名称, common/master 启动时由 DefaultTimeLocation 派生.
	DefaultTimeZone = utils.ChinaTimeZone

	// DefaultTimeLocation 是应用启动时区, 默认固定 UTC+8, 不读取宿主或 TZ.
	// 应用只能在 common.InitTimeLocation 或框架启动前覆盖, 不支持运行期切换.
	DefaultTimeLocation = utils.ChinaTimeLocation

	// ZeroTimeCST 保留固定中国时区的零值哨兵, 不随应用选择的时区变化.
	ZeroTimeCST = time.Date(1, 1, 1, 0, 0, 0, 0, utils.ChinaTimeLocation)
	ZeroTimeUTC = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
)
