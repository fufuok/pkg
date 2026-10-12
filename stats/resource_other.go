//go:build !linux

package stats

import (
	"fmt"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// mainProcess 仅在 mainCache 锁内访问, 保留跨次调用的 CPU 差分基线.
var mainProcess *process.Process

// collectSystemStats 保留非 Linux 的基础 CPU/内存统计, 不调用会在 Windows 启动后台采样的 load.Avg.
// Linux 专有负载和系统文件句柄指标正常缺省, 不写入 ResourceErrors.
func collectSystemStats() map[string]any {
	values := make(map[string]any)
	if percentages, err := cpu.Percent(0, false); err == nil && len(percentages) > 0 {
		values["CPUPercent"] = fmt.Sprintf("%.2f%%", percentages[0])
	} else {
		addStatsError(values, "ResourceErrors", "cpu")
	}
	if memory, err := mem.VirtualMemory(); err == nil && memory != nil {
		setSystemMemory(values, memory.Total, memory.Available)
	} else {
		addStatsError(values, "ResourceErrors", "memory")
	}
	return values
}

// collectMainStats 保留非 Linux 的基础进程统计, 不调用 Connections/OpenFiles 等明细枚举.
// NumConnections 的 int(0) 为未采集占位; FDUsed/FDSoftLimit 未采集时缺省.
func collectMainStats() map[string]any {
	// 保留旧字段的类型与失败零值契约, 采集有效性由 ResourceErrors 区分.
	values := map[string]any{
		"NumConnections": 0, "NumOpenFiles": 0, "NumThreads": int32(0),
		"CPUPercent": "0.00%", "MemPercent": "0.00%",
	}
	if mainProcess == nil {
		p, err := process.NewProcess(int32(os.Getpid()))
		if err != nil {
			addStatsError(values, "ResourceErrors", "process")
			return values
		}
		mainProcess = p
	}
	// 仅调用锁定依赖有真实线程实现的平台, 其他平台保留 int32(0) 未采集占位.
	// Darwin 无 CGO 时沿用依赖的 ps 读取, 不承诺所有平台都直接调用内核API.
	switch runtime.GOOS {
	case "windows", "darwin", "freebsd":
		if threads, err := mainProcess.NumThreads(); err == nil {
			values["NumThreads"] = threads
		} else {
			addStatsError(values, "ResourceErrors", "threads")
		}
	}
	if percentage, err := mainProcess.Percent(0); err == nil {
		values["CPUPercent"] = fmt.Sprintf("%.2f%%", percentage)
	} else {
		addStatsError(values, "ResourceErrors", "cpu")
	}
	if memory, err := mainProcess.MemoryInfo(); err == nil && memory != nil {
		setProcessMemory(values, memory.RSS, memory.VMS, memory.Swap)
		if host, err := mem.VirtualMemory(); err == nil && host != nil && host.Total > 0 {
			values["MemPercent"] = fmt.Sprintf("%.2f%%", 100*float64(memory.RSS)/float64(host.Total))
		} else {
			addStatsError(values, "ResourceErrors", "memory_percent")
		}
	} else {
		addStatsError(values, "ResourceErrors", "memory")
	}
	return values
}

// collectNetworkStats 非 Linux 只读取支持的接口计数, 不调用未实现的 ProtoCounters 或 TcpExt.
func collectNetworkStats() map[string]any {
	values := make(map[string]any)
	counters, err := net.IOCounters(true)
	if err != nil {
		addStatsError(values, "Errors", "interfaces")
		return values
	}
	interfaces := make(map[string]any, len(counters))
	for _, counter := range counters {
		interfaces[counter.Name] = map[string]any{
			"BytesRecv": counter.BytesRecv, "BytesSent": counter.BytesSent,
			"PacketsRecv": counter.PacketsRecv, "PacketsSent": counter.PacketsSent,
			"ErrorsRecv": counter.Errin, "ErrorsSent": counter.Errout,
			"DropsRecv": counter.Dropin, "DropsSent": counter.Dropout,
		}
	}
	values["Interfaces"] = interfaces
	return values
}
