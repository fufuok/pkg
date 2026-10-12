//go:build linux

package stats

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const fdReadBatchSize = 256

// collectMainStats 读取当前进程的 status, stat, fd 和 limits, 不枚举系统连接表.
// FD 总数允许采样自身和并发变化的偏差; NumConnections 的 int(0) 仅为未采集占位.
func collectMainStats() map[string]any {
	// 保留旧字段的类型与失败零值契约, 采集有效性由 ResourceErrors 区分.
	values := map[string]any{
		"NumConnections": 0, "NumOpenFiles": 0, "NumThreads": int32(0),
		"CPUPercent": "0.00%", "MemPercent": "0.00%",
	}
	collectMainStatus(values)
	collectMainCPU(values)
	collectProcessFDs(values)
	data, err := os.ReadFile(procPath("self/limits"))
	if err == nil {
		var limit uint64
		limit, err = parseFDSoftLimit(string(data))
		if err == nil && limit > 0 {
			values["FDSoftLimit"] = limit
		}
	}
	if err != nil {
		addStatsError(values, "ResourceErrors", "fd_limit")
	}
	return values
}

// collectMainStatus 从一次 status 读取提取线程与内存, 单项缺失不会丢掉其他资源.
// VmSwap 在旧内核缺失时近似为0, RSS/VMS 未提供时保持内存未知.
func collectMainStatus(values map[string]any) {
	status, err := readProcValues("self/status", []string{"Threads", "VmRSS", "VmSize", "VmSwap"})
	if err != nil {
		addStatsError(values, "ResourceErrors", "status")
		return
	}
	if threads, ok := status["Threads"]; ok {
		values["NumThreads"] = int32(threads)
	} else {
		addStatsError(values, "ResourceErrors", "threads")
	}
	rss, rssOK := status["VmRSS"]
	vms, vmsOK := status["VmSize"]
	if !rssOK || !vmsOK {
		addStatsError(values, "ResourceErrors", "memory")
		return
	}
	setProcessMemory(values, rss*1024, vms*1024, status["VmSwap"]*1024)
	total, _, err := readMemoryInfo()
	if err != nil {
		addStatsError(values, "ResourceErrors", "memory_percent")
		return
	}
	values["MemPercent"] = fmt.Sprintf("%.2f%%", 100*float64(rss*1024)/float64(total))
}

// collectMainCPU 用进程 tick 与系统 tick 差分估算使用率, 避免假设 USER_HZ 或调用 getconf.
// 核数稳定时100%约为占满一个逻辑 CPU, 多核可超过100%; 首次采样及计数回退时返回0.
// 结果为两次成功读取之间的均值, 访问空窗可超过30秒, 缓存过期不清空差分基线.
func collectMainCPU(values map[string]any) {
	current, cpuErr := readCPUTime()
	data, statErr := os.ReadFile(procPath("self/stat"))
	if cpuErr != nil || statErr != nil {
		addStatsError(values, "ResourceErrors", "cpu")
		return
	}
	ticks, err := parseProcessTicks(string(data))
	if err != nil {
		addStatsError(values, "ResourceErrors", "cpu")
		return
	}
	percent := float64(0)
	if lastMainCPU.total > 0 && current.total > lastMainCPU.total && ticks >= lastMainTicks {
		percent = 100 * float64(ticks-lastMainTicks) * float64(current.cores) / float64(current.total-lastMainCPU.total)
	}
	values["CPUPercent"] = fmt.Sprintf("%.2f%%", percent)
	lastMainCPU, lastMainTicks = current, ticks
}

// collectProcessFDs 分批计数 FD 目录, 不读取链接目标或区分文件, socket, pipe.
// 完整读取后才发布总量; 采样自身目录 FD 和并发变化允许少量偏差.
func collectProcessFDs(values map[string]any) {
	dir, err := os.Open(procPath("self/fd"))
	if err != nil {
		addStatsError(values, "ResourceErrors", "file_descriptors")
		return
	}
	count, err := countFDNames(dir.Readdirnames)
	_ = dir.Close()
	if err != nil {
		addStatsError(values, "ResourceErrors", "file_descriptors")
		return
	}
	values["NumOpenFiles"], values["FDUsed"] = count, int32(count)
}

// countFDNames 逐批读取到 EOF 才返回总量, 避免一次保留全部目录名称.
// EOF 可与最后一批名称同时返回; 其他错误丢弃部分计数并保留错误链.
func countFDNames(readNames func(int) ([]string, error)) (int, error) {
	count := 0
	for {
		names, err := readNames(fdReadBatchSize)
		count += len(names)
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("read process file descriptors: %w", err)
		}
	}
}

// parseProcessTicks 跳过可能包含空格或右括号的 comm, 只取 utime/stime 两列.
func parseProcessTicks(text string) (uint64, error) {
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return 0, errors.New("invalid process stat format")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 13 {
		return 0, errors.New("missing process CPU ticks")
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid process user tick: %w", err)
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid process system tick: %w", err)
	}
	return user + system, nil
}

// parseFDSoftLimit 只读取 Max open files 软限制, unlimited 正常省略为未知.
func parseFDSoftLimit(text string) (uint64, error) {
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(line, "Max open files") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return 0, errors.New("invalid open files limit format")
		}
		if fields[3] == "unlimited" {
			return 0, nil
		}
		limit, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid open files soft limit: %w", err)
		}
		return limit, nil
	}
	return 0, errors.New("open files limit not found")
}
