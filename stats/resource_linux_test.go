//go:build linux

package stats

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestProcCollections 验证全部 Linux 指标使用 HOST_PROC, 并保持公开字段的动态类型和粗略口径.
func TestProcCollections(t *testing.T) {
	root := setupProcFixture(t)
	preserveStatsCache(t, &mainCache)
	main := MainStats()
	assertMapValues(t, main, map[string]any{
		"NumThreads": int32(4), "NumConnections": 0, "NumOpenFiles": 4,
		"FDUsed": int32(4), "FDSoftLimit": uint64(1024), "MemRSSBytes": uint64(100 * 1024),
		"MemPercent": "10.00%", "CPUPercent": "0.00%",
	})
	system := collectSystemStats()
	assertMapValues(t, system, map[string]any{
		"Load1": 0.5, "Load5": 1.5, "Load15": 2.5,
		"MemTotalBytes": uint64(1000 * 1024), "MemAvailableBytes": uint64(600 * 1024),
		"MemUsedPercent": "40.00%", "FileHandlesUsed": uint64(93), "FileHandlesLimit": uint64(200), "CPUPercent": "0.00%",
	})
	network := collectNetworkStats()
	iface := network["Interfaces"].(map[string]any)["fixture0"].(map[string]any)
	assertMapValues(t, iface, map[string]any{"BytesRecv": uint64(1), "PacketsRecv": uint64(2), "ErrorsRecv": uint64(3), "DropsRecv": uint64(4), "BytesSent": uint64(9), "PacketsSent": uint64(10), "ErrorsSent": uint64(11), "DropsSent": uint64(12)})
	tcp := network["TCP"].(map[string]int64)
	udp := network["UDP"].(map[string]int64)
	if tcp["OutSegs"] != 10 || tcp["RetransSegs"] != 2 || tcp["ListenDrops"] != 33 || tcp["ListenOverflows"] != 11 || udp["InDatagrams"] != 5 || len(tcp) != 4 || len(udp) != 6 {
		t.Fatalf("network counters = %#v", network)
	}
	for _, values := range []map[string]any{main, system, network} {
		if values["Errors"] != nil || values["ResourceErrors"] != nil {
			t.Fatalf("unexpected fixture error: %#v", values)
		}
	}
	// 删除 memory 信息应只影响内存字段, 进程 FD/CPU 以及运行时字段仍能读取.
	if err := os.Remove(filepath.Join(root, "self/status")); err != nil {
		t.Fatal(err)
	}
	mainCache.values = nil
	failed := MetricStats()["Main"].(map[string]any)
	if failed["MemRSS"] != nil || failed["MemVMS"] != nil || failed["MemSwap"] != nil || failed["MemRSSBytes"] != nil || failed["FDUsed"] != int32(4) || failed["NumGoroutine"] == nil {
		t.Fatalf("partially failed process snapshot = %#v", failed)
	}
}

// TestProcCPUIntervals 验证 guest 不重复计数、单核百分比口径以及计数回退后的重新建基线.
func TestProcCPUIntervals(t *testing.T) {
	root := setupProcFixture(t)
	collectSystemStats()
	collectMainStats()
	writeProcFixture(t, root, "stat", "cpu 200 0 0 1800 0 0 0 0 900 400\ncpu0 0\ncpu1 0\n")
	writeProcFixture(t, root, "self/stat", fixtureProcessStat(110, 5))
	if got := collectSystemStats()["CPUPercent"]; got != "10.00%" {
		t.Fatalf("system CPU = %v", got)
	}
	if got := collectMainStats()["CPUPercent"]; got != "20.00%" {
		t.Fatalf("process CPU = %v", got)
	}
	writeProcFixture(t, root, "stat", "cpu 100 0 0 900 0 0 0 0 500 200\ncpu0 0\ncpu1 0\n")
	writeProcFixture(t, root, "self/stat", fixtureProcessStat(10, 5))
	if got := collectSystemStats()["CPUPercent"]; got != "0.00%" {
		t.Fatalf("reset system CPU = %v", got)
	}
	if got := collectMainStats()["CPUPercent"]; got != "0.00%" {
		t.Fatalf("reset process CPU = %v", got)
	}
}

// TestProcPartialFailures 验证缺失和损坏只使对应字段缺省, 旧 int 计数保留0回退.
func TestProcPartialFailures(t *testing.T) {
	root := setupProcFixture(t)
	writeProcFixture(t, root, "net/netstat", "TcpExt: ListenDrops ListenOverflows\nTcpExt: bad 1\n")
	network := collectNetworkStats()
	if network["Interfaces"] == nil || network["UDP"] == nil || network["TCP"].(map[string]int64)["OutSegs"] != 10 || !slices.Contains(network["Errors"].([]string), "tcp_ext") {
		t.Fatalf("partial network = %#v", network)
	}
	writeProcFixture(t, root, "self/status", "Threads: 4\nVmRSS: bad kB\n")
	writeProcFixture(t, root, "self/limits", "Max open files unlimited unlimited files\n")
	// 将 FD 目录改为普通文件, 稳定复现不可枚举, 不依赖 root 下无效的权限测试.
	if err := os.RemoveAll(filepath.Join(root, "self/fd")); err != nil {
		t.Fatal(err)
	}
	writeProcFixture(t, root, "self/fd", "unreadable directory\n")
	main := collectMainStats()
	if main["MemRSS"] != nil || main["FDUsed"] != nil || main["FDSoftLimit"] != nil || main["NumOpenFiles"] != 0 || main["NumConnections"] != 0 {
		t.Fatalf("failed process fields = %#v", main)
	}
	if !slices.Contains(main["ResourceErrors"].([]string), "status") || !slices.Contains(main["ResourceErrors"].([]string), "file_descriptors") {
		t.Fatalf("failed process errors = %#v", main)
	}
}

// TestLegacyProcessFailureTypes 验证旧进程字段读取失败时仍保留原类型零占位.
// 错误分组说明零值不是实测结果, 新增内存字节字段继续省略未知值.
func TestLegacyProcessFailureTypes(t *testing.T) {
	root := setupProcFixture(t)
	for _, name := range []string{"self/status", "self/stat"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	values := collectMainStats()
	assertMapValues(t, values, map[string]any{
		"NumThreads": int32(0), "CPUPercent": "0.00%", "MemPercent": "0.00%",
		"NumOpenFiles": 4, "NumConnections": 0,
	})
	errs, _ := values["ResourceErrors"].([]string)
	if !slices.Contains(errs, "status") || !slices.Contains(errs, "cpu") || values["MemRSSBytes"] != nil {
		t.Fatalf("failed process compatibility = %#v", values)
	}
}

// TestProcMemoryFallback 验证旧内核粗略可用量与超总量截断, 不产生负使用量.
func TestProcMemoryFallback(t *testing.T) {
	root := setupProcFixture(t)
	for _, tc := range []struct {
		text string
		want uint64
	}{
		{"MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 200 kB\nCached: 300 kB\n", 600 * 1024},
		{"MemTotal: 1000 kB\nMemAvailable: 1200 kB\n", 1000 * 1024},
	} {
		writeProcFixture(t, root, "meminfo", tc.text)
		total, available, err := readMemoryInfo()
		if err != nil || total != 1000*1024 || available != tc.want {
			t.Fatalf("memory = %d/%d err:%v", available, total, err)
		}
	}
}

// TestProcParsers 验证包含空格/括号的进程名和协议字段重排, 错误格式不得伪造正常数据.
func TestProcParsers(t *testing.T) {
	ticks, err := parseProcessTicks(fixtureProcessStat(11, 22))
	if err != nil || ticks != 33 {
		t.Fatalf("process ticks = %d err:%v", ticks, err)
	}
	for _, text := range []string{"no comm", "1 (name) S 0", fixtureProcessStat(1, 2)[:5]} {
		if _, err := parseProcessTicks(text); err == nil {
			t.Fatalf("accepted process stat %q", text)
		}
	}
	used, limit, err := parseSystemFileHandles("100 7 200\n")
	if err != nil || used != 93 || limit != 200 {
		t.Fatalf("file-nr = %d/%d err:%v", used, limit, err)
	}
	for _, text := range []string{"100 200 300", "100", "bad 1 2"} {
		if _, _, err := parseSystemFileHandles(text); err == nil {
			t.Fatalf("accepted file-nr %q", text)
		}
	}
	got, err := parseProtocolLine("ListenOverflows SyncookiesSent ListenDrops", "11 ignored 33", []string{"ListenOverflows", "ListenDrops"})
	if err != nil || got["ListenOverflows"] != 11 || got["ListenDrops"] != 33 || len(got) != 2 {
		t.Fatalf("TcpExt = %#v err:%v", got, err)
	}
	if _, err := parseProtocolLine("ListenDrops ListenOverflows", "11", []string{"ListenDrops"}); err == nil {
		t.Fatal("accepted mismatched protocol columns")
	}
	if _, err := parseFDSoftLimit("no limit"); err == nil {
		t.Fatal("accepted missing fd limit")
	}
}

// TestSocketFDsCountedInTotal 验证 socket pair 与 dup 仍计入 FD 总量, 不再输出连接数.
func TestSocketFDsCountedInTotal(t *testing.T) {
	t.Setenv("HOST_PROC", "/proc")
	before := map[string]any{}
	collectProcessFDs(before)
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(pair[0]); _ = syscall.Close(pair[1]) })
	dup, err := syscall.Dup(pair[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(dup) })
	after := map[string]any{}
	collectProcessFDs(after)
	if after["FDUsed"] != before["FDUsed"].(int32)+3 || after["NumOpenFiles"] != before["NumOpenFiles"].(int)+3 || after["NumConnections"] != nil {
		t.Fatalf("FD total delta = before:%#v after:%#v", before, after)
	}
}

// TestFDCountCompletion 验证多批和末批 EOF 都计入, 中途错误不能返回部分正常总量.
func TestFDCountCompletion(t *testing.T) {
	readErr := errors.New("fixture directory read failed")
	for _, tc := range []struct {
		name     string
		finalErr error
		want     int
	}{
		{"complete", io.EOF, 3},
		{"failed", readErr, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			count, err := countFDNames(func(batch int) ([]string, error) {
				calls++
				if batch <= 0 || calls > 2 {
					t.Fatalf("invalid read: batch=%d calls=%d", batch, calls)
				}
				if calls == 1 {
					return []string{"1", "2"}, nil
				}
				return []string{"3"}, tc.finalErr
			})
			if count != tc.want || (tc.finalErr == io.EOF && err != nil) || (tc.finalErr == readErr && !errors.Is(err, readErr)) {
				t.Fatalf("count=%d err=%v, want count=%d final=%v", count, err, tc.want, tc.finalErr)
			}
		})
	}
	if count, err := countFDNames(func(int) ([]string, error) { return nil, io.EOF }); count != 0 || err != nil {
		t.Fatalf("empty directory count=%d err=%v", count, err)
	}
}

// TestFDCountBatches 验证真实目录跨批读取及 EOF 时完整发布, 不依赖链接目标可读.
func TestFDCountBatches(t *testing.T) {
	root := setupProcFixture(t)
	for i := range 600 {
		writeProcFixture(t, root, fmt.Sprintf("self/fd/%d", 1000+i), "")
	}
	values := collectMainStats()
	assertMapValues(t, values, map[string]any{"NumOpenFiles": 604, "FDUsed": int32(604), "NumConnections": 0})
	if values["ResourceErrors"] != nil {
		t.Fatalf("complete FD directory errors = %#v", values)
	}
}

// TestProcCPUAccessIntervals 验证30秒和300秒访问窗口使用同窗口 tick 增量, 不清空过期基线.
// 窗口由两核每核100tick/s的累计值模拟, 不需要等待真实采样间隔.
func TestProcCPUAccessIntervals(t *testing.T) {
	for _, seconds := range []int{30, 300} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			root := setupProcFixture(t)
			preserveStatsCache(t, &systemCache)
			preserveStatsCache(t, &mainCache)
			systemCache.read(collectSystemStatsOf, "CollectedAt")
			MainStats()
			writeProcFixture(t, root, "stat", fmt.Sprintf("cpu %d 0 0 %d 0 0 0 0\ncpu0 0\ncpu1 0\n", 100+100*seconds, 900+100*seconds))
			writeProcFixture(t, root, "self/stat", fixtureProcessStat(uint64(10+100*seconds), 5))
			for _, cache := range []*statsCache{&systemCache, &mainCache} {
				cache.refreshedAt = time.Now().Add(-time.Duration(seconds) * time.Second)
			}
			system, main := systemCache.read(collectSystemStatsOf, "CollectedAt"), MainStats()
			if system["CPUPercent"] != "50.00%" || main["CPUPercent"] != "100.00%" {
				t.Fatalf("window=%ds system=%v main=%v", seconds, system["CPUPercent"], main["CPUPercent"])
			}
		})
	}
}

// setupProcFixture 提供最小 procfs, 故意不含连接明细表, 并恢复 CPU 差分状态.
// HOST_PROC, 缓存和基线是进程级状态, 使用者必须串行并在 cleanup 前结束采集调用.
func setupProcFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOST_PROC", root)
	oldSystem, oldMain, oldTicks := lastSystemCPU, lastMainCPU, lastMainTicks
	lastSystemCPU, lastMainCPU, lastMainTicks = cpuTime{}, cpuTime{}, 0
	t.Cleanup(func() { lastSystemCPU, lastMainCPU, lastMainTicks = oldSystem, oldMain, oldTicks })
	files := map[string]string{
		"loadavg":        "0.5 1.5 2.5 1/2 3\n",
		"meminfo":        "MemTotal: 1000 kB\nMemAvailable: 600 kB\n",
		"stat":           "cpu 100 0 0 900 0 0 0 0 500 200\ncpu0 0\ncpu1 0\n",
		"sys/fs/file-nr": "100 7 200\n",
		"self/status":    "Name: name\nThreads: 4\nVmRSS: 100 kB\nVmSize: 200 kB\nVmSwap: 0 kB\n",
		"self/stat":      fixtureProcessStat(10, 5),
		"self/limits":    "Limit Soft Limit Hard Limit Units\nMax open files 1024 2048 files\n",
		"net/dev":        "Inter-| Receive | Transmit\nfixture0: 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16\n",
		"net/snmp":       "Ip: Unknown\nIp: ignored\nTcp: OutSegs RetransSegs Unknown\nTcp: 10 2 ignored\nUdp: InDatagrams OutDatagrams InErrors RcvbufErrors SndbufErrors NoPorts Unknown\nUdp: 5 6 7 8 9 10 ignored\n",
		"net/netstat":    "TcpExt: ListenOverflows SyncookiesSent ListenDrops\nTcpExt: 11 ignored 33\n",
	}
	for name, text := range files {
		writeProcFixture(t, root, name, text)
	}
	dir := filepath.Join(root, "self/fd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, target := range []string{"socket:[1]", "socket:[1]", "pipe:[2]", "/tmp/file"} {
		if err := os.Symlink(target, filepath.Join(dir, fmt.Sprint(20+i))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// writeProcFixture 写入 LF 测试文件, 创建子目录以模拟 procfs 层次.
func writeProcFixture(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fixtureProcessStat 构造 comm 含右括号和空格的 stat, utime/stime 位于内核固定列.
func fixtureProcessStat(user, system uint64) string {
	return fmt.Sprintf("123 (a ) b) S %s%d %d\n", strings.Repeat("0 ", 10), user, system)
}
