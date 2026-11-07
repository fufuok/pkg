package common

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// TestLoggerWriterReuse 使用子进程观察真实 Roller 后台任务, 避免测试进程持有日志文件.
func TestLoggerWriterReuse(t *testing.T) {
	output, err := runCommonSubprocess(t, "TestLoggerWriterReuseHelper", "PKG_COMMON_LOGGER_REUSE_HELPER", t.TempDir())
	if err != nil {
		t.Fatalf("logger reuse helper: %v\n%s", err, output)
	}
}

// TestLoggerWriterReuseHelper 验证非文件参数重载不再创建 Roller, 旧 logger 仍可写入.
func TestLoggerWriterReuseHelper(t *testing.T) {
	root := os.Getenv("PKG_COMMON_LOGGER_REUSE_HELPER")
	if root == "" {
		return
	}
	cfg := prepareCommonConfig(t)
	config.Debug = false
	cfg.LogConf.File = filepath.Join(root, "app.log")
	cfg.LogConf.NoPretty = true
	cfg.LogConf.NoColor = true
	cfg.LogConf.Level = int(zerolog.InfoLevel)
	cfg.LogConf.Burst = 1
	cfg.LogConf.PeriodDuration = time.Hour
	before := countLoggerMills()
	initLogger()
	waitLoggerMills(t, before+1)
	initial := countLoggerMills()
	initialWriter := logFileWriter
	old := Log()
	for i := range 20 {
		cfg.LogConf.Level = int(zerolog.InfoLevel) + i%2
		cfg.LogConf.Burst = uint32(i + 1)
		cfg.LogConf.PostAPI = "https://example.invalid/log/" + strings.Repeat("x", i)
		cfg.LogConf.NoPretty = i%2 == 0
		if err := loadLogger(); err != nil {
			t.Fatalf("reload non-file parameters: %v", err)
		}
		if logFileWriter != initialWriter {
			t.Fatal("non-file parameters replaced the active file writer")
		}
	}
	// 新建协程先调度后才会出现在 profile, 等待旧实现的重复构造完全可观察.
	for range 20 {
		runtime.Gosched()
	}
	if got := countLoggerMills(); got != initial {
		t.Fatalf("non-file reload grew Roller mills: got %d, want %d", got, initial)
	}
	unchanged := Log()
	if err := loadLogger(); err != nil || Log() != unchanged {
		t.Fatalf("unchanged configuration rebuilt the logger: err=%v", err)
	}
	old.Info().Msg("Old logger still writes JSON")
	Log().Info().Msg("New logger keeps current level")
	Log().Warn().Msg("New logger writes pretty")
	body, err := os.ReadFile(cfg.LogConf.File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"M":"Old logger still writes JSON"`)) ||
		!bytes.Contains(body, []byte("New logger writes pretty")) ||
		bytes.Contains(body, []byte("New logger keeps current level")) {
		t.Fatalf("reloaded or retained logger output changed: %s", body)
	}

	// Debug 是输出目标选择条件, 即使 LogConf 完全相同也必须重建 logger 包装.
	fileWriter := logFileWriter
	last := Log()
	config.Debug = true
	if err := loadLogger(); err != nil {
		t.Fatal(err)
	}
	if Log() == last || !logCurrentDebug || logFileWriter != fileWriter {
		t.Fatal("switching to console did not replace the logger while retaining the file writer")
	}
	last = Log()
	config.Debug = false
	if err := loadLogger(); err != nil {
		t.Fatal(err)
	}
	if Log() == last || logCurrentDebug || logFileWriter != fileWriter {
		t.Fatal("switching back to file did not reuse the existing writer")
	}

	// 每个轮转参数都参与资源选择; 真正变化仍按原行为构建独立 Roller.
	for _, change := range []func(){
		func() { cfg.LogConf.MaxSize++ },
		func() { cfg.LogConf.MaxAge++ },
		func() { cfg.LogConf.MaxBackups++ },
	} {
		previous := logFileWriter
		change()
		if err := loadLogger(); err != nil {
			t.Fatal(err)
		}
		if logFileWriter == previous {
			t.Fatal("changed rotation settings unexpectedly reused the previous Roller")
		}
	}

	previousWriter, previousConf, previousLogConf := logFileWriter, logFileConf, logCurrentConf
	previousLogger := Log()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.LogConf.File = filepath.Join(blocked, "logs", "new.log")
	if err := loadLogger(); err == nil {
		t.Fatal("blocked file target unexpectedly succeeded")
	}
	if logFileWriter != previousWriter || logFileConf != previousConf ||
		Log() != previousLogger || logCurrentConf != previousLogConf {
		t.Fatal("failed writer replacement changed active resources or cached configuration")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := loadLogger(); err != nil {
		t.Fatalf("unchanged failed target did not retry after repair: %v", err)
	}
	if logFileWriter == previousWriter || logCurrentConf != cfg.LogConf {
		t.Fatal("repaired target did not publish the new writer")
	}
	Log().Warn().Msg("Repaired writer receives current logs")
	old.Info().Msg("Retained logger keeps original target")
	body, err = os.ReadFile(cfg.LogConf.File)
	if err != nil || !bytes.Contains(body, []byte("Repaired writer receives current logs")) ||
		bytes.Contains(body, []byte("Retained logger keeps original target")) {
		t.Fatalf("new file target output: err=%v, body=%s", err, body)
	}
	body, err = os.ReadFile(filepath.Join(root, "app.log"))
	if err != nil || !bytes.Contains(body, []byte("Retained logger keeps original target")) {
		t.Fatalf("retained logger lost its original writer: err=%v, body=%s", err, body)
	}
}

// countLoggerMills 仅统计锁定 lumberjack 实例的后台协程, 排除其他运行时任务波动.
func countLoggerMills() int {
	var stacks bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
	return strings.Count(stacks.String(), "lumberjack/v3.(*Roller).millRun(")
}

// waitLoggerMills 等待构造器已启动的后台协程进入 profile, 不推断普通协程总量.
func waitLoggerMills(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for countLoggerMills() < want {
		if time.Now().After(deadline) {
			t.Fatalf("Roller mills did not start: got %d, want at least %d", countLoggerMills(), want)
		}
		runtime.Gosched()
	}
}
