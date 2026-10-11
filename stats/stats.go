package stats

import (
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"strings"
	"sync"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool"
	"github.com/fufuok/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/master"
)

var (
	mainProcess *process.Process
	mainOnce    sync.Once
	mainStatsMu sync.Mutex

	// resourceCacheTTL 限制连接和文件描述符枚举频率. 统计接口没有调用时不会启动后台采集.
	resourceCacheTTL = 30 * time.Second

	// memoryInfoOf 读取当前进程内存信息.
	// 默认走 gopsutil Process.MemoryInfo; 包内测试可替换以覆盖失败和 nil 返回, 不支持并行改写.
	memoryInfoOf        = (*process.Process).MemoryInfo
	numConnectionsOf    = (*process.Process).Connections
	numFDsOf            = (*process.Process).NumFDs
	openFilesOf         = (*process.Process).OpenFiles
	rlimitOf            = (*process.Process).Rlimit
	systemFileHandlesOf = systemFileHandles

	networkIOCountersOf    = net.IOCounters
	networkProtoCountersOf = net.ProtoCounters
	tcpExtCountersOf       = readTCPExtCounters
)

// mainResourceSnapshot 仅保存需要枚举内核表的进程资源. CPU Percent 保留原有每次调用基线,
// 因此不放入缓存, 也不把整个 MainStats 快照化.
type mainResourceSnapshot struct {
	collectedAt  time.Time
	refreshedAt  time.Time
	connections  int
	fdUsed       int32
	fdOK         bool
	fdSoftLimit  uint64
	limitOK      bool
	resourceErrs []string
}

var mainResourceCache struct {
	sync.Mutex
	snapshot mainResourceSnapshot
}

type networkInterfaceSnapshot struct {
	bytesRecv, bytesSent                         uint64
	packetsRecv, packetsSent                     uint64
	errorsRecv, errorsSent, dropsRecv, dropsSent uint64
}

type networkSnapshot struct {
	collectedAt time.Time
	refreshedAt time.Time
	interfaces  map[string]networkInterfaceSnapshot
	udp         map[string]int64
	tcp         map[string]int64
	errors      []string
}

var networkCache struct {
	sync.Mutex
	snapshot networkSnapshot
}

// SYSStats 系统信息. 主机动态资源最多每30秒读取一次, 其余应用和时间字段仍实时生成.
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
	for key, value := range systemResourceStats() {
		host[key] = value
	}
	stats["Host"] = host

	return stats
}

// systemResourceStats 读取主机动态资源并返回展示值和机器可读原始值.
// 失败项保持缺省, 避免把采集失败伪装成零值; 缓存锁覆盖整个刷新过程, 并发只执行一次.
func systemResourceStats() map[string]any {
	resourceCacheMu.Lock()
	defer resourceCacheMu.Unlock()
	if time.Since(resourceCacheAt) < resourceCacheTTL && resourceCacheValues != nil {
		return cloneStatsMap(resourceCacheValues)
	}

	values := make(map[string]any)
	var resourceErrors []string
	if avg, err := load.Avg(); err == nil {
		values["LoadAvg"] = fmt.Sprintf("%.2f, %.2f, %.2f", avg.Load1, avg.Load5, avg.Load15)
		values["Load1"], values["Load5"], values["Load15"] = avg.Load1, avg.Load5, avg.Load15
	} else {
		resourceErrors = append(resourceErrors, "load")
	}
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		values["CPUPercent"] = fmt.Sprintf("%.2f%%", cpuPercent[0])
	} else {
		resourceErrors = append(resourceErrors, "cpu")
	}
	if memStat, err := mem.VirtualMemory(); err == nil {
		values["MemTotal"] = utils.HumanIBytes(memStat.Total)
		values["MemAvailable"] = utils.HumanIBytes(memStat.Available)
		values["MemUsed"] = utils.HumanIBytes(memStat.Used)
		values["MemUsedPercent"] = fmt.Sprintf("%.2f%%", memStat.UsedPercent)
		values["MemTotalBytes"], values["MemAvailableBytes"] = memStat.Total, memStat.Available
	} else {
		resourceErrors = append(resourceErrors, "memory")
	}
	if used, limit, err := systemFileHandlesOf(); err == nil {
		values["FileHandlesUsed"], values["FileHandlesLimit"] = used, limit
	} else {
		resourceErrors = append(resourceErrors, "file_handles")
	}
	if len(resourceErrors) > 0 {
		values["ResourceErrors"] = resourceErrors
	}
	values["CollectedAt"] = common.GTimeNow()
	resourceCacheAt, resourceCacheValues = time.Now(), values
	return cloneStatsMap(values)
}

var (
	resourceCacheMu     sync.Mutex
	resourceCacheAt     time.Time
	resourceCacheValues map[string]any
)

// cloneStatsMap 复制浅层统计映射, 避免调用方修改缓存中的切片或嵌套数据.
func cloneStatsMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for key, value := range src {
		if errs, ok := value.([]string); ok {
			dst[key] = append([]string(nil), errs...)
			continue
		}
		dst[key] = value
	}
	return dst
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

// MainStats 主程序系统指标.
// MemoryInfo 失败时省略 MemRSS/MemVMS/MemSwap, 其余字段仍返回; 进程句柄不可用时返回 nil.
// 连接和文件描述符枚举最多每30秒执行一次, 以避免每个 stats 请求重复读取内核表.
func MainStats() map[string]any {
	mainStatsMu.Lock()
	defer mainStatsMu.Unlock()
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
	// 失败时 memInfo 可能为 nil, 后续组装必须判空, 不能忽略错误后直接解引用.
	memInfo, _ := memoryInfoOf(mainProcess)
	memPercent, _ := mainProcess.MemoryPercent()

	resource := mainResourceStats(mainProcess)

	stats := map[string]any{
		"ProcessPid":   mainProcess.Pid,
		"NumThreads":   numThreads,
		"CPUPercent":   fmt.Sprintf("%.2f%%", cpuPercent),
		"MemPercent":   fmt.Sprintf("%.2f%%", memPercent),
		"NumGoroutine": runtime.NumGoroutine(),
		"NumCgoCall":   utils.Comma(runtime.NumCgoCall()),
		"GoMaxProcs":   runtime.GOMAXPROCS(0),
	}
	stats["NumConnections"] = resource.connections
	// 保留旧字段和旧的失败回退0行为. Linux 优先使用 NumFDs, 不再逐项 Readlink.
	stats["NumOpenFiles"] = resource.fdUsed
	if resource.fdOK {
		stats["FDUsed"] = resource.fdUsed
	}
	if resource.limitOK {
		stats["FDSoftLimit"] = resource.fdSoftLimit
	}
	if len(resource.resourceErrs) > 0 {
		stats["ResourceErrors"] = append([]string(nil), resource.resourceErrs...)
	}
	// MemoryInfo 失败时只省略三块内存字段, 其余进程指标仍返回.
	if memInfo != nil {
		stats["MemRSS"] = utils.HumanIBytes(memInfo.RSS)
		stats["MemVMS"] = utils.HumanIBytes(memInfo.VMS)
		stats["MemSwap"] = utils.HumanIBytes(memInfo.Swap)
		stats["MemRSSBytes"] = memInfo.RSS
	}
	stats["OSCollectedAt"] = resource.collectedAt
	return stats
}

// mainResourceStats 获取连接、FD和软限制快照. 失败的新增字段保持未知, 旧连接字段仍按兼容约定返回0.
func mainResourceStats(p *process.Process) mainResourceSnapshot {
	now := time.Now()
	mainResourceCache.Lock()
	defer mainResourceCache.Unlock()
	if now.Sub(mainResourceCache.snapshot.refreshedAt) < resourceCacheTTL && !mainResourceCache.snapshot.refreshedAt.IsZero() {
		return cloneMainResourceSnapshot(mainResourceCache.snapshot)
	}

	snapshot := mainResourceSnapshot{collectedAt: common.GTimeNow(), refreshedAt: now}
	if conns, err := numConnectionsOf(p); err == nil {
		snapshot.connections = len(conns)
	} else {
		snapshot.resourceErrs = append(snapshot.resourceErrs, "connections")
	}
	if fds, err := numFDsOf(p); err == nil {
		snapshot.fdUsed, snapshot.fdOK = fds, true
	} else if files, fallbackErr := openFilesOf(p); fallbackErr == nil {
		// 仅在 NumFDs 不支持的平台回退, 避免改变旧版本的公开字段行为.
		snapshot.fdUsed, snapshot.fdOK = int32(len(files)), true
	} else {
		snapshot.resourceErrs = append(snapshot.resourceErrs, "file_descriptors")
	}
	if limits, err := rlimitOf(p); err == nil {
		for _, limit := range limits {
			if limit.Resource == process.RLIMIT_NOFILE && limit.Soft > 0 && limit.Soft < ^uint64(0) {
				snapshot.fdSoftLimit, snapshot.limitOK = limit.Soft, true
				break
			}
		}
	} else {
		snapshot.resourceErrs = append(snapshot.resourceErrs, "fd_limit")
	}
	mainResourceCache.snapshot = snapshot
	return cloneMainResourceSnapshot(snapshot)
}

// cloneMainResourceSnapshot 复制资源快照, 防止调用方修改错误切片.
func cloneMainResourceSnapshot(src mainResourceSnapshot) mainResourceSnapshot {
	src.resourceErrs = append([]string(nil), src.resourceErrs...)
	return src
}

// NetworkStats 返回主机网络命名空间的累计计数. 函数是可选入口, 只在调用且缓存过期时读取系统计数,
// 不启动后台任务; 返回值不代表当前进程独占流量, 调用方应使用 CollectedAt 做相邻差分.
func NetworkStats() map[string]any {
	now := time.Now()
	networkCache.Lock()
	defer networkCache.Unlock()
	if now.Sub(networkCache.snapshot.refreshedAt) < resourceCacheTTL && !networkCache.snapshot.refreshedAt.IsZero() {
		return networkSnapshotMap(networkCache.snapshot)
	}

	snapshot := collectNetworkSnapshot()
	networkCache.snapshot = snapshot
	return networkSnapshotMap(snapshot)
}

// collectNetworkSnapshot 读取三类累计计数; 每类失败只影响自己的字段.
func collectNetworkSnapshot() networkSnapshot {
	snapshot := networkSnapshot{collectedAt: common.GTimeNow(), refreshedAt: time.Now(), interfaces: make(map[string]networkInterfaceSnapshot)}
	collectNetworkInterfaces(&snapshot)
	collectNetworkProtocols(&snapshot)
	collectTCPExt(&snapshot)
	return snapshot
}

// collectNetworkInterfaces 读取各接口累计字节、包、错误和丢弃计数, 不做接口合计.
func collectNetworkInterfaces(snapshot *networkSnapshot) {
	counters, err := networkIOCountersOf(true)
	if err != nil {
		snapshot.errors = append(snapshot.errors, "interfaces")
		return
	}
	for _, counter := range counters {
		snapshot.interfaces[counter.Name] = networkInterfaceSnapshot{
			bytesRecv: counter.BytesRecv, bytesSent: counter.BytesSent,
			packetsRecv: counter.PacketsRecv, packetsSent: counter.PacketsSent,
			errorsRecv: counter.Errin, errorsSent: counter.Errout,
			dropsRecv: counter.Dropin, dropsSent: counter.Dropout,
		}
	}
}

// collectNetworkProtocols 读取 IPv4 UDP/TCP 累计计数, 不将协议计数解释为进程独占流量.
func collectNetworkProtocols(snapshot *networkSnapshot) {
	protocols, err := networkProtoCountersOf([]string{"udp", "tcp"})
	if err != nil {
		snapshot.errors = append(snapshot.errors, "protocols")
		return
	}
	for _, protocol := range protocols {
		if protocol.Protocol != "udp" && protocol.Protocol != "tcp" {
			continue
		}
		values := make(map[string]int64, len(protocol.Stats))
		for key, value := range protocol.Stats {
			values[key] = value
		}
		if protocol.Protocol == "udp" {
			snapshot.udp = values
		} else {
			snapshot.tcp = values
		}
	}
}

// collectTCPExt 追加 Linux TcpExt 的监听溢出和丢弃计数, 其他平台保留基础 TCP 数据.
func collectTCPExt(snapshot *networkSnapshot) {
	tcpExt, err := tcpExtCountersOf()
	if err == nil {
		if snapshot.tcp == nil {
			snapshot.tcp = make(map[string]int64)
		}
		for key, value := range tcpExt {
			snapshot.tcp[key] = value
		}
		return
	}
	// TcpExt 在非 Linux 平台不存在, 不让它覆盖已成功的接口和 UDP/TCP 基础计数.
	if len(snapshot.tcp) > 0 || len(snapshot.interfaces) > 0 {
		snapshot.errors = append(snapshot.errors, "tcp_ext")
	}
}

// networkSnapshotMap 将内部快照转换为独立 map, 保证返回对象与缓存相互隔离.
func networkSnapshotMap(snapshot networkSnapshot) map[string]any {
	result := map[string]any{"CollectedAt": snapshot.collectedAt}
	interfaces := make(map[string]any, len(snapshot.interfaces))
	for name, counter := range snapshot.interfaces {
		interfaces[name] = map[string]any{
			"BytesRecv": counter.bytesRecv, "BytesSent": counter.bytesSent,
			"PacketsRecv": counter.packetsRecv, "PacketsSent": counter.packetsSent,
			"ErrorsRecv": counter.errorsRecv, "ErrorsSent": counter.errorsSent,
			"DropsRecv": counter.dropsRecv, "DropsSent": counter.dropsSent,
		}
	}
	if len(interfaces) > 0 {
		result["Interfaces"] = interfaces
	}
	if values := protocolSnapshotMap(snapshot.udp, []string{"InDatagrams", "OutDatagrams", "InErrors", "RcvbufErrors", "SndbufErrors", "NoPorts"}); len(values) > 0 {
		result["UDP"] = values
	}
	if values := protocolSnapshotMap(snapshot.tcp, []string{"OutSegs", "RetransSegs", "ListenOverflows", "ListenDrops"}); len(values) > 0 {
		result["TCP"] = values
	}
	if len(snapshot.errors) > 0 {
		result["Errors"] = append([]string(nil), snapshot.errors...)
	}
	return result
}

// protocolSnapshotMap 仅导出看板约定字段, 避免把平台私有协议字段变成公开契约.
func protocolSnapshotMap(values map[string]int64, keys []string) map[string]int64 {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]int64, len(keys))
	for _, key := range keys {
		if value, ok := values[key]; ok {
			result[key] = value
			continue
		}
		// gopsutil 使用 /proc/net/snmp 的原始小写字段名, 对外统一 PascalCase.
		if value, ok := values[strings.ToLower(key)]; ok {
			result[key] = value
		}
	}
	return result
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
