package stats

import (
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
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
	now := common.GTimeNow()
	uptime := now.Sub(common.StartTime)
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
			"Uptime":       uptime.String(),
			"UptimeSecond": int(uptime.Seconds()),
			"StartTime":    common.StartTime,
			"ClockOffset":  common.GetClockOffset().String(),
			"GlobalTime":   now.Format(time.RFC3339Nano),
			"LocalTime":    time.Now(),
		},
	}

	host := map[string]any{
		"InternalIPv4": common.InternalIPv4,
		"ExternalIPv4": common.ExternalIPv4,
		"NumCpus":      runtime.NumCPU(),
		"NodeInfo":     config.Config().NodeConf.NodeInfo,
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

// getMemoryStats 获取内存相关指标
func getMemoryStats(ms *runtime.MemStats) map[string]any {
	// 内存碎片化指标计算
	// (HeapIdle - HeapReleased) 表示已保留但未使用的内存，HeapSys 表示系统为堆分配的总内存
	trueFragRatio := float64(ms.HeapIdle-ms.HeapReleased) / float64(ms.HeapSys+1) // +1 防止除零
	return map[string]any{
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
}

// getGCStats 获取GC相关指标
func getGCStats(ms *runtime.MemStats) map[string]any {
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

	return map[string]any{
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
}

// processFloat64Histogram 处理 float64 直方图类型的指标
func processFloat64Histogram(hist *metrics.Float64Histogram, key string, schedMetrics map[string]any) {
	if hist == nil {
		return
	}

	totalCount := uint64(0)
	for _, count := range hist.Counts {
		totalCount += count
	}

	if totalCount == 0 {
		// 无数据时的默认输出
		if key == "GCPauses" {
			// 将GCPauses移至GC子map中
			if gcMap, ok := schedMetrics["GC"].(map[string]any); ok {
				gcMap["PausesHistogram"] = map[string]any{
					"TotalCount":  0,
					"BucketCount": len(hist.Buckets),
				}
			}
		} else {
			schedMetrics[key] = map[string]any{
				"TotalCount":  0,
				"BucketCount": len(hist.Buckets),
			}
		}
		return
	}

	// 计算平均、最大等统计量
	sum := 0.0
	maxValue := 0.0

	for i, count := range hist.Counts {
		if count > 0 && i < len(hist.Buckets) {
			// 使用桶的上限作为该桶内事件的值
			bucketValue := hist.Buckets[i]
			sum += bucketValue * float64(count)
			if bucketValue > maxValue {
				maxValue = bucketValue
			}
		}
	}

	average := sum / float64(totalCount)

	// 根据指标类型提供更有针对性的输出
	switch key {
	case "GCPauses":
		if gcMap, ok := schedMetrics["GC"].(map[string]any); ok {
			gcMap["PausesHistogram"] = map[string]any{
				"TotalCount":  totalCount,                         // GC 暂停事件总计数
				"AverageMs":   fmt.Sprintf("%.3f", average*1000),  // 平均暂停时间(毫秒)
				"MaxMs":       fmt.Sprintf("%.3f", maxValue*1000), // 最大暂停时间(毫秒)
				"BucketCount": len(hist.Buckets),                  // 直方图的桶数
			}
		}
	case "SchedulingLatencies":
		schedMetrics[key] = map[string]any{
			"TotalCount":  totalCount,                          // 调度事件总计数
			"AverageMs":   fmt.Sprintf("%.3fms", average*1e3),  // 平均调度延迟(毫秒)
			"MaxMs":       fmt.Sprintf("%.3fms", maxValue*1e3), // 最大调度延迟(毫秒)
			"BucketCount": len(hist.Buckets),                   // 直方图的桶数
		}
	default:
		// 通用直方图输出
		schedMetrics[key] = map[string]any{
			"TotalCount":  totalCount,        // 事件总计数
			"BucketCount": len(hist.Buckets), // 直方图的桶数
		}
	}
}

// getSchedulerStats 获取调度器相关指标
func getSchedulerStats(gcStats map[string]any) map[string]any {
	schedMetrics := map[string]any{
		"GC":                 gcStats,
		"GoPoolFree":         ants.Free(),
		"GoPoolRunning":      ants.Running(),
		"GoPoolIdleWorkers":  ants.IdleWorkers(),
		"GoPoolTotalWorkers": ants.TotalWorkers(),
	}

	// 定义指标名称到映射键的映射
	metricMap := map[string]string{
		// 调度器相关指标
		"/sched/goroutines-created:goroutines":   "GoroutinesCreated",
		"/sched/goroutines/not-in-go:goroutines": "GoroutinesNotInGo",
		"/sched/goroutines/runnable:goroutines":  "GoroutinesRunnable",
		"/sched/goroutines/running:goroutines":   "GoroutinesRunning",
		"/sched/goroutines/waiting:goroutines":   "GoroutinesWaiting",
		"/sched/threads/total:threads":           "ThreadsTotal",
		"/sched/goroutines:goroutines":           "GoroutinesTotal",
		"/sched/gomaxprocs:threads":              "GOMAXPROCS",
		"/sched/latencies:seconds":               "SchedulingLatencies",

		// 内存分配相关指标
		"/gc/heap/allocs:bytes": "HeapAllocsBytes",
		"/gc/heap/frees:bytes":  "HeapFreesBytes",

		// GC 相关指标
		"/gc/pauses:seconds": "GCPauses",
	}

	// 构建 metricSamples
	metricSamples := make([]metrics.Sample, 0, len(metricMap))
	for name := range metricMap {
		metricSamples = append(metricSamples, metrics.Sample{Name: name})
	}

	// 调用 runtime/metrics 包的 Read 函数
	metrics.Read(metricSamples)

	// 存储原始数值用于分析
	rawMetrics := make(map[string]uint64)
	for _, sample := range metricSamples {
		// 使用映射获取对应的键名
		key, ok := metricMap[sample.Name]
		if !ok {
			continue
		}

		switch sample.Value.Kind() {
		default:
			continue
		case metrics.KindUint64:
			value := sample.Value.Uint64()
			rawMetrics[sample.Name] = value
			// 对内存分配相关指标使用 HumanGBMB 格式化
			if key == "HeapAllocsBytes" || key == "HeapFreesBytes" {
				schedMetrics[key] = utils.HumanGBMB(value)
			} else {
				schedMetrics[key] = utils.Commau(value)
			}
		case metrics.KindFloat64:
			value := sample.Value.Float64()
			schedMetrics[key] = fmt.Sprintf("%.4f", value)
		case metrics.KindFloat64Histogram:
			// 处理直方图类型的指标
			hist := sample.Value.Float64Histogram()
			processFloat64Histogram(hist, key, schedMetrics)
		case metrics.KindBad:
			// 无效的指标值类型，跳过处理
			continue
		}
	}

	return schedMetrics
}

// MetricStats 主程序运行指标
func MetricStats() map[string]any {
	result := make(map[string]any)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	result["Main"] = MainStats()
	result["Memory"] = getMemoryStats(&ms)

	gcStats := getGCStats(&ms)
	result["Scheduler"] = getSchedulerStats(gcStats)

	return result
}

// MetricStatsWithBytesPoolStats 带内存池统计指标
func MetricStatsWithBytesPoolStats(topN int, ps ...*bytespool.CapacityPools) map[string]any {
	ms := MetricStats()
	ms["BytesPool"] = BytesPoolStats(topN, ps...)
	return ms
}

func BytesPoolStats(topN int, ps ...*bytespool.CapacityPools) map[string]any {
	ss := bytespool.RuntimeStatsSummary(topN, ps...)
	ms := map[string]any{
		"Capacity": fmt.Sprintf(
			"[%s, %s]",
			utils.HumanIntIBytes(ss.MinSize),
			utils.HumanIntIBytes(ss.MaxSize),
		),
		"NewBytes":    utils.HumanIBytes(ss.NewBytes),
		"OutBytes":    utils.HumanIBytes(ss.OutBytes),
		"OutCount":    utils.Commau(ss.OutCount),
		"ReusedBytes": utils.HumanIBytes(ss.ReusedBytes),
	}
	if topN > 0 {
		ms["TopPools"] = ss.TopPools
	}
	return ms
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
		"GoMaxProcs":     runtime.GOMAXPROCS(0),
		"NumConnections": connCount,
		"NumOpenFiles":   fdCount,
	}
	return stats
}

func WebStats() map[string]any {
	cfg := config.Config().WebConf
	stats := map[string]any{
		// HTTP 服务是否关闭了减少内存占用选项
		"DisableReduceMemoryUsage": cfg.DisableReduceMemoryUsage,
		// HTTP 服务是否关闭了 keep-alive
		"DisableKeepalive": cfg.DisableKeepalive,
		// 是否启用了 HTTPS
		"HTTPS": cfg.ServerHttpsAddr != "",
		// 请求体大小限制
		"BodyLimit": utils.HumanIntIBytes(cfg.BodyLimit),
	}
	return stats
}
