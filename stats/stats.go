package stats

import (
	"fmt"
	"runtime"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool"
	"github.com/fufuok/utils"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/master"
)

// SYSStats 系统信息
func SYSStats() map[string]any {
	return map[string]any{
		"AppName":        config.AppName,
		"Version":        config.Version,
		"GitCommit":      config.GitCommit,
		"Uptime":         time.Since(common.StartTime).String(),
		"StartTime":      common.StartTime,
		"ClockOffset":    common.GetClockOffset().String(),
		"GlobalTime":     common.GTimeNowString(time.RFC3339Nano),
		"LocalTime":      time.Now(),
		"Debug":          config.Debug,
		"LogLevel":       zerolog.Level(config.Config().LogConf.Level).String(),
		"ConfigModTime":  master.ConfigModTime,
		"ConfigLoadTime": master.ConfigLoadTime,
		"GoVersion":      config.GoVersion,
		"DebVersion":     config.DebVersion,
		"GoMaxProcs":     runtime.GOMAXPROCS(0),
		"NumCpus":        runtime.NumCPU(),
		"NumGoroutine":   runtime.NumGoroutine(),
		"NumCgoCall":     utils.Comma(runtime.NumCgoCall()),
		"InternalIPv4":   common.InternalIPv4,
		"ExternalIPv4":   common.ExternalIPv4,
		"JSON":           json.Name,
		"GoPool":         ants.Running(),
	}
}

// MEMStats 内存信息
func MEMStats() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	bs := bytespool.RuntimeStats()
	return map[string]any{
		// 程序启动后累计申请的字节数
		"TotalAlloc":  ms.TotalAlloc,
		"TotalAlloc_": utils.HumanGBMB(ms.TotalAlloc),
		// 虚拟占用, 总共向系统申请的字节数
		"HeapSys":  ms.HeapSys,
		"HeapSys_": utils.HumanGBMB(ms.HeapSys),
		// 使用中或未使用, 但未被 GC 释放的对象的字节数
		"HeapAlloc":  ms.HeapAlloc,
		"HeapAlloc_": utils.HumanGBMB(ms.HeapAlloc),
		// 使用中的对象的字节数
		"HeapInuse":  ms.HeapInuse,
		"HeapInuse_": utils.HumanGBMB(ms.HeapInuse),
		// 已释放的内存, 还没被堆再次申请的内存
		"HeapReleased":  ms.HeapReleased,
		"HeapReleased_": utils.HumanGBMB(ms.HeapReleased),
		// 没被使用的内存, 包含了 HeapReleased, 可被再次申请和使用
		"HeapIdle":  ms.HeapIdle,
		"HeapIdle_": utils.HumanGBMB(ms.HeapIdle),
		// 分配的对象数
		"HeapObjects":  ms.HeapObjects,
		"HeapObjects_": utils.Commau(ms.HeapObjects),
		// 下次 GC 的阈值, 当 HeapAlloc 达到该值触发 GC
		"NextGC":  ms.NextGC,
		"NextGC_": utils.HumanGBMB(ms.NextGC),
		// 上次 GC 时间
		"LastGC": time.Unix(0, int64(ms.LastGC)).Format(time.RFC3339Nano),
		// GC 次数
		"NumGC": utils.Commau(uint64(ms.NumGC)),
		// 被强制 GC 的次数
		"NumForcedGC": ms.NumForcedGC,
		// GC 暂停时间总量
		"PauseTotalNs": fmt.Sprintf("%.3fs", float64(ms.PauseTotalNs)/1000/1000/1000),
		// 上次 GC 暂停时间
		"PauseNs": fmt.Sprintf("%.3fms", float64(ms.PauseNs[(ms.NumGC+255)%256])/1000/1000),
		// 字节池使用信息
		"BytesPool": map[string]string{
			"Big":   utils.HumanGBMB(bs["Big"]),
			"New":   utils.HumanGBMB(bs["New"]),
			"Reuse": utils.HumanGBMB(bs["Reuse"]),
		},
	}
}

func WebStats() map[string]any {
	cfg := config.Config()
	return map[string]any{
		// HTTP 服务是否关闭了减少内存占用选项
		"DisableReduceMemoryUsage": cfg.WebConf.DisableReduceMemoryUsage,
		// HTTP 服务是否关闭了 keep-alive
		"DisableKeepalive": cfg.WebConf.DisableKeepalive,
		// 是否启用了 HTTPS
		"HTTPS": cfg.WebConf.ServerHttpsAddr != "",
		// 请求体大小限制
		"BodyLimit": utils.HumanIntIBytes(cfg.WebConf.BodyLimit),
	}
}
