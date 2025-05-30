package stats

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool"
	"github.com/fufuok/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/master"
)

var (
	mainProcess *process.Process
	mainOnce    sync.Once
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

// MetricStats 主程序运行指标
func MetricStats() map[string]any {
	return map[string]any{
		"Memory": MemStats(),
		"Main":   MainStats(),
	}
}

// MemStats 内存信息
func MemStats() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	bs := bytespool.RuntimeStats()
	return map[string]any{
		// 程序启动后累计申请的字节数
		"TotalAlloc":  ms.TotalAlloc,
		"TotalAlloc_": utils.HumanIBytes(ms.TotalAlloc),
		// 虚拟占用, 总共向系统申请的字节数
		"HeapSys":  ms.HeapSys,
		"HeapSys_": utils.HumanIBytes(ms.HeapSys),
		// 使用中或未使用, 但未被 GC 释放的对象的字节数
		"HeapAlloc":  ms.HeapAlloc,
		"HeapAlloc_": utils.HumanIBytes(ms.HeapAlloc),
		// 使用中的对象的字节数
		"HeapInuse":  ms.HeapInuse,
		"HeapInuse_": utils.HumanIBytes(ms.HeapInuse),
		// 已释放的内存, 还没被堆再次申请的内存
		"HeapReleased":  ms.HeapReleased,
		"HeapReleased_": utils.HumanIBytes(ms.HeapReleased),
		// 没被使用的内存, 包含了 HeapReleased, 可被再次申请和使用
		"HeapIdle":  ms.HeapIdle,
		"HeapIdle_": utils.HumanIBytes(ms.HeapIdle),
		// 分配的对象数
		"HeapObjects":  ms.HeapObjects,
		"HeapObjects_": utils.Commau(ms.HeapObjects),
		// 下次 GC 的阈值, 当 HeapAlloc 达到该值触发 GC
		"NextGC":  ms.NextGC,
		"NextGC_": utils.HumanIBytes(ms.NextGC),
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
			"Big":   utils.HumanIBytes(bs["Big"]),
			"New":   utils.HumanIBytes(bs["New"]),
			"Reuse": utils.HumanIBytes(bs["Reuse"]),
		},
	}
}

// MainStats 主程序系统指标
func MainStats() map[string]any {
	mainOnce.Do(func() {
		if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
			mainProcess = p
		}
	})
	if mainProcess == nil {
		return nil
	}

	ctx := context.Background()
	numThreads, err := mainProcess.NumThreadsWithContext(ctx)
	if err != nil {
		return nil
	}
	memPercent, err := mainProcess.MemoryPercentWithContext(ctx)
	if err != nil {
		return nil
	}
	cpuPercent, err := mainProcess.PercentWithContext(ctx, 0)
	if err != nil {
		return nil
	}
	memInfo, err := mainProcess.MemoryInfoWithContext(ctx)
	if err != nil {
		return nil
	}
	return map[string]any{
		"ProcessPid": mainProcess.Pid,
		"NumThreads": numThreads,
		"MemPercent": utils.Round(float64(memPercent), 2),
		"CPUPercent": utils.Round(cpuPercent, 2),
		"MemRSS":     utils.HumanIBytes(memInfo.RSS),
		"MemVMS":     utils.HumanIBytes(memInfo.VMS),
		"MemSwap":    utils.HumanIBytes(memInfo.Swap),
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
