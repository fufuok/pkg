// 演示在业务启动前统一使用美国纽约时区, 不读取配置文件或启动常驻服务.
package main

import (
	"fmt"
	"log"
	"time"
	_ "time/tzdata" // 内嵌 IANA 数据, 避免依赖宿主是否安装时区数据库.

	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

// main 在任何应用 goroutine、协程池任务及 logger/cron 创建前选择时区.
// 本示例只展示标准库和业务时钟, 最后等待默认池退出, 不进入完整服务生命周期.
func main() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatalf("Failed to load application time location: %v", err)
	}
	config.DefaultTimeLocation = loc
	if err := common.InitTimeLocation(); err != nil {
		// 初始化失败时不能继续创建业务资源.
		log.Fatalf("Failed to initialize application time location: %v", err)
	}

	fmt.Printf("standard: %s (%s)\n", time.Now().Format(time.RFC3339), time.Local)
	fmt.Printf("business: %s (%s)\n", common.GTimeNow().Format(time.RFC3339), config.DefaultTimeLocation)
	// 同一 UTC 时刻的冬夏样本展示 IANA 规则, 不把纽约误写为全年固定 UTC-5.
	winter := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC).In(time.Local)
	summer := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC).In(time.Local)
	fmt.Printf("winter: %s\n", winter.Format("2006-01-02T15:04:05-07:00 MST"))
	fmt.Printf("summer: %s\n", summer.Format("2006-01-02T15:04:05-07:00 MST"))

	// 示例执行后立即退出, 等待默认池后台计时结束再结束进程.
	if err := ants.ReleaseTimeout(5 * time.Second); err != nil {
		log.Fatalf("Failed to stop default worker pool: %v", err)
	}
}
