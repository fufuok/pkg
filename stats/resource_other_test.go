//go:build !linux

package stats

import (
	"runtime"
	"slices"
	"testing"

	"github.com/shirou/gopsutil/v3/process"
)

// TestProcessThreadFailure 验证 Windows 读取失败仍保留旧字段原类型零占位, 并记录失败项.
// 测试串行替换进程实例, 使用不存在的PID触发依赖错误, 不模拟存活进程退出.
func TestProcessThreadFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("failure probe requires Windows process snapshots")
	}
	mainCache.Lock()
	defer mainCache.Unlock()
	old := mainProcess
	defer func() { mainProcess = old }()
	mainProcess = &process.Process{Pid: -1}
	values := collectMainStats()
	assertMapValues(t, values, map[string]any{
		"NumThreads": int32(0), "CPUPercent": "0.00%", "MemPercent": "0.00%",
	})
	errs, _ := values["ResourceErrors"].([]string)
	if !slices.Contains(errs, "threads") || !slices.Contains(errs, "cpu") || !slices.Contains(errs, "memory") {
		t.Fatalf("process failure = %#v, want thread, CPU and memory errors", values)
	}
}

// TestUnsupportedResourcesAreOmitted 验证非 Linux 扩展能力不制造失败项或伪造 FDUsed.
func TestUnsupportedResourcesAreOmitted(t *testing.T) {
	system := collectSystemStats()
	for _, key := range []string{"LoadAvg", "Load1", "Load5", "Load15", "FileHandlesUsed", "FileHandlesLimit"} {
		if system[key] != nil {
			t.Fatalf("unsupported system field %s = %v", key, system[key])
		}
	}
	main := collectMainStats()
	if main["FDUsed"] != nil || main["FDSoftLimit"] != nil || main["NumOpenFiles"] != 0 || main["NumConnections"] != 0 {
		t.Fatalf("unsupported process fields = %#v", main)
	}
	switch runtime.GOOS {
	case "windows", "darwin", "freebsd":
	default:
		errs, _ := main["ResourceErrors"].([]string)
		if main["NumThreads"] != int32(0) || slices.Contains(errs, "threads") {
			t.Fatalf("unsupported thread collection = %#v", main)
		}
	}
	network := collectNetworkStats()
	if network["UDP"] != nil || network["TCP"] != nil {
		t.Fatalf("unsupported protocol counters = %#v", network)
	}
	if errs, ok := network["Errors"].([]string); ok {
		for _, group := range errs {
			if group != "interfaces" {
				t.Fatalf("unsupported protocol error = %s", group)
			}
		}
	}
}
