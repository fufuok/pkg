package stats

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool"
	"github.com/fufuok/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
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
	stats := map[string]any{
		// 应用信息
		"App": map[string]any{
			"AppName":    config.AppName,
			"Version":    config.Version,
			"GitCommit":  config.GitCommit,
			"GoVersion":  config.GoVersion,
			"DebVersion": config.DebVersion,
			"JSON":       json.Name,
		},
		// 配置信息
		"Config": map[string]any{
			"LogLevel":       zerolog.Level(config.Config().LogConf.Level).String(),
			"ConfigModTime":  master.ConfigModTime,
			"ConfigLoadTime": master.ConfigLoadTime,
			"Debug":          config.Debug,
		},
		// 时间信息
		"Time": map[string]any{
			"Uptime":      time.Since(common.StartTime).String(),
			"StartTime":   common.StartTime,
			"ClockOffset": common.GetClockOffset().String(),
			"GlobalTime":  common.GTimeNowString(time.RFC3339Nano),
			"LocalTime":   time.Now(),
		},
	}

	host := map[string]any{
		"InternalIPv4": common.InternalIPv4,
		"ExternalIPv4": common.ExternalIPv4,
		"NumCpus":      runtime.NumCPU(),
	}
	// 系统负载 (LoadAvg)
	if avg, err := load.Avg(); err == nil {
		host["LoadAvg"] = fmt.Sprintf("%.2f, %.2f, %.2f", avg.Load1, avg.Load5, avg.Load15)
	}
	// CPU 使用率
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		host["CPUPercent"] = fmt.Sprintf("%.2f%%", cpuPercent[0])
	}
	// 系统内存信息
	if memStat, err := mem.VirtualMemory(); err == nil {
		host["MemTotal"] = utils.HumanIBytes(memStat.Total)
		host["MemAvailable"] = utils.HumanIBytes(memStat.Available)
		host["MemUsed"] = utils.HumanIBytes(memStat.Used)
		host["MemUsedPercent"] = fmt.Sprintf("%.2f%%", memStat.UsedPercent)
	}
	stats["Host"] = host

	return stats
}

// MetricStats 主程序运行指标
func MetricStats() map[string]any {
	metrics := make(map[string]any)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	metrics["Main"] = MainStats()

	// 内存碎片化指标计算
	// (HeapIdle - HeapReleased) 表示已保留但未使用的内存，HeapSys 表示系统为堆分配的总内存
	trueFragRatio := float64(ms.HeapIdle-ms.HeapReleased) / float64(ms.HeapSys+1) // +1 防止除零
	metrics["Memory"] = map[string]any{
		// TotalAlloc: 程序启动后累计向操作系统申请的字节数（包括已释放的内存）
		// Total bytes allocated for heap objects since program start (累计分配的堆对象字节数)
		"TotalAlloc": utils.HumanIBytes(ms.TotalAlloc),
		// HeapSys: 堆内存虚拟占用大小，即总共向系统申请的字节数
		// Total bytes obtained from system for heap (堆向操作系统申请的总字节数)
		"HeapSys": utils.HumanIBytes(ms.HeapSys),
		// HeapAlloc: 当前堆内存分配的字节数（使用中+未使用但未被GC释放的）
		// Bytes of allocated heap objects (当前已分配且未被GC回收的堆对象字节数)
		"HeapAlloc": utils.HumanIBytes(ms.HeapAlloc),
		// HeapInuse: 当前堆内存中正在使用的字节数（包括已分配但未被GC回收的）
		// Bytes in use by heap objects (当前正在使用的堆内存字节数)
		"HeapInuse": utils.HumanIBytes(ms.HeapInuse),
		// HeapReleased: 已释放回操作系统的堆内存字节数（可被再次分配）
		// Bytes released to the OS (已释放回操作系统但未被再次分配的堆内存字节数)
		"HeapReleased": utils.HumanIBytes(ms.HeapReleased),
		// HeapIdle: 堆内存中空闲的字节数（包括已释放但未返还给系统的内存）
		// Bytes in idle spans (未被使用的堆内存，包括HeapReleased，可被再次分配)
		"HeapIdle": utils.HumanIBytes(ms.HeapIdle),
		// HeapObjects: 当前堆内存中已分配的堆对象数量, 堆中存活的对象数量
		// Number of allocated heap objects (当前堆对象数量)
		"HeapObjects": utils.Commau(ms.HeapObjects),
		// StackInuse: 栈内存使用字节数
		// Bytes used by stack memory (当前所有Goroutine栈内存总和)
		"StackInuse": utils.HumanIBytes(ms.StackInuse),
		// MSpanInuse: MSpan 结构使用的内存字节数（用于管理小对象的元数据）
		// Bytes used by MSpan structures (管理堆内存的元数据结构MSpan占用的字节数)
		"MSpanInuse": utils.HumanIBytes(ms.MSpanInuse),
		// MCacheInuse: MCache 结构使用的内存字节数（每个P的本地缓存，用于快速分配小对象）
		// Bytes used by MCache structures (每个P的本地缓存MCache占用的字节数)
		"MCacheInuse": utils.HumanIBytes(ms.MCacheInuse),
		// FragmentationPercent: 堆内存碎片化百分比，反映已保留但未使用的内存占比
		// Heap fragmentation percent: (HeapIdle-HeapReleased)/HeapSys (堆内存碎片率，反映未被使用但未释放的堆内存占比)
		"FragmentationPercent": fmt.Sprintf("%.2f%%", trueFragRatio*100),
	}

	// Pause durations (ms) for the last 5 GCs (最近5次GC的暂停时间，单位ms)
	var lastPauses []string
	// For calculating the max pause (用于计算最大暂停时间)
	var lastPauseValues []float64
	num := int(ms.NumGC)
	for i := 0; i < 5 && i < num; i++ {
		idx := (num - 1 - i) % 256
		pause := ms.PauseNs[idx]
		lastPauses = append(lastPauses, fmt.Sprintf("%.3fms", float64(pause)/1e6))
		lastPauseValues = append(lastPauseValues, float64(pause)/1e6)
	}

	// Time since last GC (距离上次GC的时间)
	timeSinceLastGC := time.Since(time.Unix(0, int64(ms.LastGC)))

	// Average GC pause duration in ms (GC平均暂停时间，单位ms)
	gcPauseAvg := float64(0)
	if ms.NumGC > 0 {
		gcPauseAvg = float64(ms.PauseTotalNs) / float64(ms.NumGC) / 1000000
	}

	// GC pressure: (NextGC-HeapAlloc)/NextGC (GC压力，越接近0表示即将触发GC)
	// GC 压力指标: 距离下次 GC 的剩余空间比例
	gcPressure := float64(ms.NextGC-ms.HeapAlloc) / float64(ms.NextGC)

	// Max GC pause duration in ms (最近5次GC的最大暂停时间，单位ms)
	maxPause := float64(0)
	if len(lastPauseValues) > 0 {
		maxPause = lastPauseValues[0]
		for _, v := range lastPauseValues {
			if v > maxPause {
				maxPause = v
			}
		}
	}
	metrics["GC"] = map[string]any{
		// NextGC: 下次GC触发的堆分配字节阈值
		// Next GC trigger threshold in bytes (下次GC触发的堆分配字节阈值)
		"NextGC": utils.HumanIBytes(ms.NextGC),
		// LastGC: 上次GC发生的时间，RFC3339格式
		// Time of last GC (上次GC发生的时间，RFC3339格式)
		"LastGC": time.Unix(0, int64(ms.LastGC)).Format(time.RFC3339Nano),
		// NumGC: GC累计次数
		// Number of completed GCs (GC累计次数)
		"NumGC": utils.Commau(uint64(ms.NumGC)),
		// NumForcedGC: 手动触发的GC次数, runtime.GC()调用次数
		// Number of forced GCs (手动触发的GC次数, runtime.GC()调用次数）
		"NumForcedGC": utils.Commau(uint64(ms.NumForcedGC)),
		// PauseTotalSeconds: GC总暂停时间，单位秒
		// Total GC pause time in seconds (GC总暂停时间，单位秒)
		"PauseTotalSeconds": fmt.Sprintf("%.3fs", float64(ms.PauseTotalNs)/1000/1000/1000),
		// LastPauseMs: 上次GC的暂停时间，单位毫秒
		// Last GC pause duration in ms (上次GC的暂停时间，单位ms)
		"LastPauseMs": fmt.Sprintf("%.3fms", float64(ms.PauseNs[(ms.NumGC+255)%256])/1000/1000),
		// PauseAvgMs: GC平均暂停时间，单位毫秒
		// Average GC pause duration in ms (GC平均暂停时间，单位ms)
		"PauseAvgMs": utils.Round(gcPauseAvg, 3),
		// PauseMaxMs: 最近5次GC的最大暂停时间，单位毫秒
		// Max GC pause duration in ms (最近5次GC的最大暂停时间，单位ms)
		"PauseMaxMs": utils.Round(maxPause, 3),
		// PressureRatio: GC压力比率，(NextGC-HeapAlloc)/NextGC，越接近0表示压力越大
		// GC pressure ratio, (NextGC-HeapAlloc)/NextGC, closer to 0 means higher pressure
		"PressureRatio": utils.Round(gcPressure, 4),
		// GCPerSecond: GC每秒触发次数, 反映 GC 频率
		// GC frequency (GC每秒触发次数, 反映 GC 频率)
		"GCPerSecond": utils.Round(float64(ms.NumGC)/time.Since(common.StartTime).Seconds(), 4),
		// PauseRecent: 最近5次GC的暂停时间列表，用于分析GC性能波动，单次>100ms需警惕STW过长
		// Pause durations (ms) for the last 5 GCs (最近5次GC的暂停时间，单位ms，单次>100ms需警惕STW过长)
		"PauseRecent": lastPauses,
		// GCCPUFraction: GC占用CPU时间的比例，反映GC对CPU资源的占用
		// Fraction of CPU time spent in GC (GC占用CPU的比例，单位百分比)
		"GCCPUFraction": fmt.Sprintf("%.2f%%", ms.GCCPUFraction*100),
		// LastGCAgo: 距离上次GC的时间间隔，用于分析GC频率
		// Time since last GC (距离上次GC的时间)
		"LastGCAgo": timeSinceLastGC.String(),
	}

	bs := bytespool.RuntimeStats()
	metrics["BytesPool"] = map[string]string{
		"Big":   utils.HumanIBytes(bs["Big"]),
		"New":   utils.HumanIBytes(bs["New"]),
		"Reuse": utils.HumanIBytes(bs["Reuse"]),
	}
	return metrics
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

	numThreads, _ := mainProcess.NumThreads()
	cpuPercent, _ := mainProcess.Percent(0)
	memInfo, _ := mainProcess.MemoryInfo()
	memPercent, _ := mainProcess.MemoryPercent()

	// 网络连接数统计
	var connCount int
	if conns, err := mainProcess.Connections(); err == nil {
		connCount = len(conns)
	}

	// 文件描述符使用情况
	var fdCount int
	if fds, err := mainProcess.OpenFiles(); err == nil {
		fdCount = len(fds)
	}

	stats := map[string]any{
		"ProcessPid":     mainProcess.Pid,
		"NumThreads":     numThreads,
		"CPUPercent":     fmt.Sprintf("%.2f%%", cpuPercent),
		"MemPercent":     fmt.Sprintf("%.2f%%", memPercent),
		"MemRSS":         utils.HumanIBytes(memInfo.RSS),
		"MemVMS":         utils.HumanIBytes(memInfo.VMS),
		"MemSwap":        utils.HumanIBytes(memInfo.Swap),
		"NumGoroutine":   runtime.NumGoroutine(),
		"NumCgoCall":     utils.Comma(runtime.NumCgoCall()),
		"GoPool":         ants.Running(),
		"GoMaxProcs":     runtime.GOMAXPROCS(0),
		"NumConnections": connCount,
		"NumOpenFiles":   fdCount,
	}
	return stats
}

func WebStats() map[string]any {
	cfg := config.Config()
	stats := map[string]any{
		// HTTP 服务是否关闭了减少内存占用选项
		"DisableReduceMemoryUsage": cfg.WebConf.DisableReduceMemoryUsage,
		// HTTP 服务是否关闭了 keep-alive
		"DisableKeepalive": cfg.WebConf.DisableKeepalive,
		// 是否启用了 HTTPS
		"HTTPS": cfg.WebConf.ServerHttpsAddr != "",
		// 请求体大小限制
		"BodyLimit": utils.HumanIntIBytes(cfg.WebConf.BodyLimit),
	}
	return stats
}
