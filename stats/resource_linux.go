//go:build linux

package stats

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var (
	// 分别由 systemCache 和 mainCache 的锁保护, 首次读取只建立 CPU 差分基线.
	lastSystemCPU cpuTime
	lastMainCPU   cpuTime
	lastMainTicks uint64
)

// cpuTime 仅保存总 CPU tick、空闲 tick 和内核逻辑 CPU 数, 不展开每核指标.
type cpuTime struct {
	total, idle uint64
	cores       int
}

// procPath 统一使用 HOST_PROC 指定的 procfs, 避免同一快照混用宿主和容器来源.
func procPath(name string) string {
	root := os.Getenv("HOST_PROC")
	if root == "" {
		root = "/proc"
	}
	return filepath.Join(root, name)
}

// collectSystemStats 只读取少量 procfs 文本, 不调用 gopsutil 或等待采样间隔.
// 内存使用量近似为总量减可用量; 首次 CPU 为0, 后续为两次成功读取之间的均值.
// 30秒只限制访问触发的刷新频率, 无访问或读取失败时 CPU 采样窗口可能更长.
func collectSystemStats() map[string]any {
	values := make(map[string]any)
	data, err := os.ReadFile(procPath("loadavg"))
	if err == nil {
		var loads [3]float64
		loads, err = parseLoadAvg(string(data))
		if err == nil {
			values["LoadAvg"] = fmt.Sprintf("%.2f, %.2f, %.2f", loads[0], loads[1], loads[2])
			values["Load1"], values["Load5"], values["Load15"] = loads[0], loads[1], loads[2]
		}
	}
	if err != nil {
		addStatsError(values, "ResourceErrors", "load")
	}
	if current, err := readCPUTime(); err == nil {
		percent := float64(0)
		if lastSystemCPU.total > 0 && current.total > lastSystemCPU.total && current.idle >= lastSystemCPU.idle {
			total, idle := current.total-lastSystemCPU.total, current.idle-lastSystemCPU.idle
			if idle <= total {
				percent = 100 * float64(total-idle) / float64(total)
			}
		}
		values["CPUPercent"] = fmt.Sprintf("%.2f%%", percent)
		lastSystemCPU = current
	} else {
		addStatsError(values, "ResourceErrors", "cpu")
	}
	if total, available, err := readMemoryInfo(); err == nil {
		setSystemMemory(values, total, available)
	} else {
		addStatsError(values, "ResourceErrors", "memory")
	}
	data, err = os.ReadFile(procPath("sys/fs/file-nr"))
	if err == nil {
		var used, limit uint64
		used, limit, err = parseSystemFileHandles(string(data))
		if err == nil {
			values["FileHandlesUsed"], values["FileHandlesLimit"] = used, limit
		}
	}
	if err != nil {
		addStatsError(values, "ResourceErrors", "file_handles")
	}
	return values
}

// parseLoadAvg 只解析 loadavg 的前三列, 忽略线程数等附加字段.
func parseLoadAvg(text string) ([3]float64, error) {
	var values [3]float64
	fields := strings.Fields(text)
	if len(fields) < len(values) {
		return values, errors.New("invalid loadavg format")
	}
	for i := range values {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return values, fmt.Errorf("invalid loadavg value: %w", err)
		}
		values[i] = value
	}
	return values, nil
}

// readProcValues 按键读取 status/meminfo 的整数, 忽略其他字段和单位, 调用方负责 kB 换算.
func readProcValues(name string, keys []string) (map[string]uint64, error) {
	data, err := os.ReadFile(procPath(name))
	if err != nil {
		return nil, err
	}
	values := make(map[string]uint64, len(keys))
	for line := range strings.SplitSeq(string(data), "\n") {
		key, text, ok := strings.Cut(line, ":")
		if !ok || !slices.Contains(keys, key) {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) == 0 {
			return nil, fmt.Errorf("missing %s in %s", key, name)
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid %s in %s: %w", key, name, err)
		}
		values[key] = value
	}
	return values, nil
}

// readMemoryInfo 优先使用 MemAvailable, 旧内核退回 Free+Buffers+Cached 的粗略估计.
func readMemoryInfo() (total, available uint64, err error) {
	values, err := readProcValues("meminfo", []string{"MemTotal", "MemAvailable", "MemFree", "Buffers", "Cached"})
	if err != nil {
		return 0, 0, err
	}
	total = values["MemTotal"] * 1024
	if total == 0 {
		return 0, 0, errors.New("MemTotal not found or zero")
	}
	available, ok := values["MemAvailable"]
	if !ok {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return total, min(total, available*1024), nil
}

// readCPUTime 读取总 CPU tick 和核数. guest 已计入 user/nice, 不再次累加.
func readCPUTime() (cpuTime, error) {
	data, err := os.ReadFile(procPath("stat"))
	if err != nil {
		return cpuTime{}, err
	}
	var value cpuTime
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return value, errors.New("invalid CPU stat format")
			}
			for i := 1; i < min(len(fields), 9); i++ {
				ticks, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					return value, fmt.Errorf("invalid CPU tick: %w", err)
				}
				value.total += ticks
				if i == 4 || i == 5 {
					value.idle += ticks
				}
			}
		} else if strings.HasPrefix(line, "cpu") {
			value.cores++
		}
	}
	if value.total == 0 || value.cores == 0 {
		return value, errors.New("CPU counters not found")
	}
	return value, nil
}

// parseSystemFileHandles 解析 file-nr 的已分配、未使用和上限, 返回全系统使用量.
func parseSystemFileHandles(text string) (used, limit uint64, err error) {
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return 0, 0, errors.New("invalid file-nr format")
	}
	var counters [3]uint64
	for i := range counters {
		counters[i], err = strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid file-nr counter: %w", err)
		}
	}
	if counters[0] < counters[1] {
		return 0, 0, errors.New("invalid file-nr counters")
	}
	return counters[0] - counters[1], counters[2], nil
}
