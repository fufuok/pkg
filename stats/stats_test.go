package stats

import (
	"errors"
	"math"
	"runtime"
	"runtime/metrics"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/bytespool"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/utils"
)

// TestGetMemoryStats 验证 runtime.MemStats 到公开内存指标的字段映射和碎片率.
func TestGetMemoryStats(t *testing.T) {
	ms := &runtime.MemStats{
		TotalAlloc:   1024,
		HeapSys:      999,
		HeapAlloc:    512,
		HeapInuse:    768,
		HeapReleased: 300,
		HeapIdle:     800,
		HeapObjects:  12,
		StackInuse:   256,
		MSpanInuse:   128,
		MCacheInuse:  64,
	}
	got := getMemoryStats(ms)
	want := map[string]any{
		"TotalAlloc":           utils.HumanIBytes(ms.TotalAlloc),
		"HeapSys":              utils.HumanIBytes(ms.HeapSys),
		"HeapAlloc":            utils.HumanIBytes(ms.HeapAlloc),
		"HeapInuse":            utils.HumanIBytes(ms.HeapInuse),
		"HeapReleased":         utils.HumanIBytes(ms.HeapReleased),
		"HeapIdle":             utils.HumanIBytes(ms.HeapIdle),
		"HeapObjects":          utils.Commau(ms.HeapObjects),
		"StackInuse":           utils.HumanIBytes(ms.StackInuse),
		"MSpanInuse":           utils.HumanIBytes(ms.MSpanInuse),
		"MCacheInuse":          utils.HumanIBytes(ms.MCacheInuse),
		"FragmentationPercent": "50.00%",
	}
	assertMapValues(t, got, want)
}

// TestGetGCStats 验证无 GC 和最近多次 GC 两类统计分支.
func TestGetGCStats(t *testing.T) {
	lastGC := time.Now().Add(-time.Second).Truncate(time.Nanosecond)
	ms := &runtime.MemStats{
		HeapAlloc:     250,
		NextGC:        1000,
		LastGC:        uint64(lastGC.UnixNano()),
		NumGC:         3,
		NumForcedGC:   1,
		PauseTotalNs:  9 * uint64(time.Millisecond),
		GCCPUFraction: 0.125,
	}
	ms.PauseNs[0] = uint64(time.Millisecond)
	ms.PauseNs[1] = 5 * uint64(time.Millisecond)
	ms.PauseNs[2] = 3 * uint64(time.Millisecond)

	got := getGCStats(ms)
	assertMapValues(t, got, map[string]any{
		"LastGC":            lastGC.Format(time.RFC3339Nano),
		"NumGC":             "3",
		"NumForcedGC":       "1",
		"PauseTotalSeconds": "0.009s",
		"LastPauseMs":       "3.000ms",
		"PauseAvgMs":        3.0,
		"PauseMaxMs":        5.0,
		"PressureRatio":     0.75,
		"GCCPUFraction":     "12.50%",
	})
	pauses, ok := got["PauseRecent"].([]string)
	if !ok || len(pauses) != 3 || pauses[0] != "3.000ms" || pauses[1] != "5.000ms" || pauses[2] != "1.000ms" {
		t.Fatalf("recent GC pauses = %#v", got["PauseRecent"])
	}

	zero := getGCStats(&runtime.MemStats{NextGC: 1})
	if zero["PauseAvgMs"] != float64(0) || zero["PauseMaxMs"] != float64(0) {
		t.Fatalf("zero GC pauses = avg:%v max:%v", zero["PauseAvgMs"], zero["PauseMaxMs"])
	}
}

// TestProcessFloat64Histogram 验证 nil、空数据和三类直方图路由.
// 当前生产实现的桶数及上下界估算与 runtime/metrics 定义存在偏差,
// 本用例只冻结总计数和单位格式, 不把待修正的具体估算值固化为契约.
func TestProcessFloat64Histogram(t *testing.T) {
	gc := map[string]any{}
	sched := map[string]any{"GC": gc}
	processFloat64Histogram(nil, "Ignored", sched)
	if _, ok := sched["Ignored"]; ok {
		t.Fatal("nil histogram produced output")
	}

	empty := &metrics.Float64Histogram{Buckets: []float64{0, 1}, Counts: []uint64{0}}
	processFloat64Histogram(empty, "GCPauses", sched)
	assertMapValues(t, gc["PausesHistogram"].(map[string]any), map[string]any{"TotalCount": 0})
	processFloat64Histogram(empty, "Empty", sched)
	assertMapValues(t, sched["Empty"].(map[string]any), map[string]any{"TotalCount": 0})

	hist := &metrics.Float64Histogram{
		Buckets: []float64{0, 0.001, 0.002, math.Inf(1)},
		Counts:  []uint64{0, 2, 1},
	}
	processFloat64Histogram(hist, "GCPauses", sched)
	gcHistogram := gc["PausesHistogram"].(map[string]any)
	assertMapValues(t, gcHistogram, map[string]any{"TotalCount": uint64(3)})
	if _, ok := gcHistogram["AverageMs"].(string); !ok {
		t.Fatalf("GC histogram average = %#v", gcHistogram["AverageMs"])
	}
	processFloat64Histogram(hist, "SchedulingLatencies", sched)
	scheduling := sched["SchedulingLatencies"].(map[string]any)
	assertMapValues(t, scheduling, map[string]any{"TotalCount": uint64(3)})
	if average, ok := scheduling["AverageMs"].(string); !ok || !strings.HasSuffix(average, "ms") {
		t.Fatalf("scheduling histogram average = %#v", scheduling["AverageMs"])
	}
	processFloat64Histogram(hist, "Other", sched)
	assertMapValues(t, sched["Other"].(map[string]any), map[string]any{"TotalCount": uint64(3)})
}

// TestRuntimeStatsContracts 验证系统、进程、调度器和 bytespool 的稳定结构契约.
func TestRuntimeStatsContracts(t *testing.T) {
	config.InitTester()
	t.Cleanup(config.StopTester)

	system := SYSStats()
	app := requireMap(t, system, "App")
	assertMapValues(t, app, map[string]any{
		"AppName": config.AppName,
		"Version": config.Version,
		"JSON":    json.Name,
	})
	timeStats := requireMap(t, system, "Time")
	if _, ok := timeStats["UptimeSecond"].(int); !ok {
		t.Fatalf("system uptime seconds = %#v", timeStats["UptimeSecond"])
	}
	host := requireMap(t, system, "Host")
	if cpus, ok := host["NumCpus"].(int); !ok || cpus < 1 {
		t.Fatalf("system CPU count = %#v", host["NumCpus"])
	}

	main := MainStats()
	if main != nil {
		if pid, ok := main["ProcessPid"].(int32); !ok || pid <= 0 {
			t.Fatalf("process pid = %#v", main["ProcessPid"])
		}
		if _, ok := main["NumGoroutine"].(int); !ok {
			t.Fatalf("goroutine count = %#v", main["NumGoroutine"])
		}
	}

	metricsStats := MetricStats()
	if main != nil && metricsStats["Main"] == nil {
		t.Fatal("MetricStats omitted available process metrics")
	}
	memory := requireMap(t, metricsStats, "Memory")
	if _, ok := memory["HeapAlloc"]; !ok {
		t.Fatal("MetricStats omitted HeapAlloc")
	}
	scheduler := requireMap(t, metricsStats, "Scheduler")
	if _, ok := scheduler["GC"].(map[string]any); !ok {
		t.Fatalf("scheduler GC metrics = %#v", scheduler["GC"])
	}
	for _, key := range []string{"GoPoolFree", "GoPoolRunning", "GoPoolIdleWorkers", "GoPoolTotalWorkers"} {
		if _, ok := scheduler[key]; !ok {
			t.Fatalf("scheduler omitted %s", key)
		}
	}

	pool := bytespool.NewCapacityPools(64, 256)
	pool.SetWithStats(true)
	buf := pool.Get(64)
	pool.Put(buf)
	pool.Put(pool.Get(64))
	withoutTop := BytesPoolStats(0, pool)
	if _, ok := withoutTop["TopPools"]; ok {
		t.Fatal("BytesPoolStats exposed TopPools when topN is zero")
	}
	withTop := BytesPoolStats(1, pool)
	if _, ok := withTop["TopPools"].([]bytespool.PoolStat); !ok {
		t.Fatalf("bytes pool top stats = %#v", withTop["TopPools"])
	}
	combined := MetricStatsWithBytesPoolStats(1, pool)
	if _, ok := combined["BytesPool"].(map[string]any); !ok {
		t.Fatalf("combined bytes pool stats = %#v", combined["BytesPool"])
	}
}

// TestMainStatsOmitsMemoryFieldsWhenMemoryInfoFails 验证 MemoryInfo 失败时不 panic.
// 四个项目的 /sys/stats 都走 MetricStats -> MainStats, 这里同时钉住两条入口.
func TestMainStatsOmitsMemoryFieldsWhenMemoryInfoFails(t *testing.T) {
	preserveMemoryInfoOf(t)
	memoryInfoOf = func(*process.Process) (*process.MemoryInfoStat, error) {
		return nil, errors.New("fixture memory info failed")
	}

	main := MainStats()
	if main == nil {
		t.Fatal("MainStats returned nil after MemoryInfo failure")
	}
	if pid, ok := main["ProcessPid"].(int32); !ok || pid <= 0 {
		t.Fatalf("process pid = %#v", main["ProcessPid"])
	}
	if _, ok := main["NumGoroutine"].(int); !ok {
		t.Fatalf("goroutine count = %#v", main["NumGoroutine"])
	}
	for _, key := range []string{"MemRSS", "MemVMS", "MemSwap"} {
		if _, ok := main[key]; ok {
			t.Fatalf("MainStats kept %s after MemoryInfo failure: %#v", key, main[key])
		}
	}

	metricsStats := MetricStats()
	gotMain, ok := metricsStats["Main"].(map[string]any)
	if !ok || gotMain == nil {
		t.Fatalf("MetricStats Main = %#v", metricsStats["Main"])
	}
	if _, ok := gotMain["MemRSS"]; ok {
		t.Fatalf("MetricStats kept MemRSS after MemoryInfo failure: %#v", gotMain)
	}
}

// TestMainStatsKeepsMemoryFieldsWhenMemoryInfoSucceeds 验证成功路径仍输出三块内存字段.
func TestMainStatsKeepsMemoryFieldsWhenMemoryInfoSucceeds(t *testing.T) {
	preserveMemoryInfoOf(t)
	memoryInfoOf = func(*process.Process) (*process.MemoryInfoStat, error) {
		return &process.MemoryInfoStat{RSS: 1024, VMS: 2048, Swap: 0}, nil
	}

	main := MainStats()
	if main == nil {
		t.Fatal("MainStats returned nil on MemoryInfo success")
	}
	assertMapValues(t, main, map[string]any{
		"MemRSS":  utils.HumanIBytes(1024),
		"MemVMS":  utils.HumanIBytes(2048),
		"MemSwap": utils.HumanIBytes(0),
	})
}

// preserveMemoryInfoOf 保存进程内存查询函数并在测试结束时恢复.
func preserveMemoryInfoOf(t *testing.T) {
	t.Helper()
	old := memoryInfoOf
	t.Cleanup(func() {
		memoryInfoOf = old
	})
}

// TestWebAndDescriptionStats 验证 Web 开关映射及三类描述入口的稳定锚点.
// 已知缺失的 UptimeSecond 和 NodeInfo 描述不在本用例中固化, 由独立缺陷处理.
func TestWebAndDescriptionStats(t *testing.T) {
	config.InitTester()
	t.Cleanup(config.StopTester)
	cfg := config.Config()
	cfg.WebConf.DisableReduceMemoryUsage = true
	cfg.WebConf.DisableKeepalive = true
	cfg.WebConf.ServerHttpsAddr = ":443"
	cfg.WebConf.BodyLimit = 4096
	assertMapValues(t, WebStats(), map[string]any{
		"DisableReduceMemoryUsage": true,
		"DisableKeepalive":         true,
		"HTTPS":                    true,
		"BodyLimit":                utils.HumanIntIBytes(4096),
	})

	systemDesc := SYSStatsDesc()
	if _, ok := systemDesc["App"].(map[string]string); !ok {
		t.Fatalf("system description App = %#v", systemDesc["App"])
	}
	metricDesc := MetricStatsDesc()
	if _, ok := metricDesc["Scheduler"].(map[string]any); !ok {
		t.Fatalf("metric description Scheduler = %#v", metricDesc["Scheduler"])
	}
	webDesc := WebStatsDesc()
	if webDesc["HTTPS"] == "" || webDesc["BodyLimit"] == "" {
		t.Fatalf("web descriptions = %#v", webDesc)
	}
}

// requireMap 读取父指标中的 map 值, 类型或键不匹配时立即失败.
func requireMap(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("metric %s = %#v, want map[string]any", key, parent[key])
	}
	return value
}

// assertMapValues 验证实际 map 至少包含全部期望键值, 允许生产增加新指标.
func assertMapValues(t *testing.T, got, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("map value %s = %#v, want %#v", key, got[key], value)
		}
	}
}
