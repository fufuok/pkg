package common

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/config"
)

const timeLocationStopTimeout = 5 * time.Second

var (
	timeLocationOnce sync.Once
	timeLocationErr  error
	timeLocation     *time.Location
)

// InitTimeLocation 在业务启动前一次性统一标准库和 pkg 的应用时区.
// 默认使用 config.DefaultTimeLocation 的固定 UTC+8; 应用可提前指定其他 Location,
// 或显式使用尚未覆盖的 time.Local 采用宿主规则. M.Start 和 master 启动入口自动调用本函数.
//
// 调用必须早于应用 goroutine、首次 ants.Submit、自建协程池及 logger/cron 的创建,
// 且不得预先替换或关闭 ants 默认池. 函数只协调当前 pkg 引入的默认池后台计时,
// 无法暂停应用或其他依赖自行启动的时间读取任务, 因而不支持运行期切换.
//
// sync.Once 仅防止重复启动赋值. 与 ants 计时读取的同步来自成功的 ReleaseTimeout,
// 不是写端加锁. 初始化失败时不得继续启动业务; 停池失败可能已将默认池关闭.
func InitTimeLocation() error {
	loc := config.DefaultTimeLocation
	if loc == nil {
		return errors.New("application time location cannot be nil")
	}

	timeLocationOnce.Do(func() {
		// time.Now 即使随后只取 UnixNano 也会读取 Local. 必须等待默认池的
		// purge/ticktock 退出, 再写全局指针; 同一 Location 无需停池或重写.
		stopped := time.Local != loc
		if stopped {
			if err := ants.ReleaseTimeout(timeLocationStopTimeout); err != nil {
				timeLocationErr = fmt.Errorf("stop default worker pool before initializing application time location: %w", err)
				return
			}
			time.Local = loc
		}

		config.DefaultTimeZone = loc.String()
		// 不转换 StartTime: Time.In 会丢失单调时钟, 展示时再按应用时区转换副本.
		timeLocation = loc
		if stopped {
			ants.Reboot()
		}
	})
	if timeLocationErr != nil {
		return timeLocationErr
	}
	if loc != timeLocation || time.Local != timeLocation {
		return errors.New("application time location cannot change after initialization")
	}
	return nil
}
