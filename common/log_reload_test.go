package common

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool/buffer"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// TestLoggerFailedReloadRetry 验证失败保留旧 logger, 修复目录后相同配置能够重试.
// 文件 logger 的资源由进程持有, 使用子进程让退出后释放文件, 不扩大生产关闭接口.
func TestLoggerFailedReloadRetry(t *testing.T) {
	output, err := runCommonSubprocess(t, "TestLoggerFailedReloadRetryHelper", "PKG_COMMON_LOGGER_RETRY_HELPER", t.TempDir())
	if err != nil {
		t.Fatalf("logger retry helper: %v\n%s", err, output)
	}
}

// TestLoggerFailedReloadRetryHelper 在真实文件路径上验证 Runtime 错误链和失败后重试.
func TestLoggerFailedReloadRetryHelper(t *testing.T) {
	root := os.Getenv("PKG_COMMON_LOGGER_RETRY_HELPER")
	if root == "" {
		return
	}
	cfg := prepareCommonConfig(t)
	config.Debug = false
	parent := filepath.Join(root, "blocked")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatalf("create blocked log directory: %v", err)
	}
	cfg.LogConf.File = filepath.Join(parent, "app.log")
	cfg.LogConf.NoPretty = true
	installCommonTestLoggers(io.Discard, zerolog.TraceLevel)
	oldLogger := Log()
	oldConf := logCurrentConf

	err := (&M{}).Runtime()
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !strings.Contains(err.Error(), "unable to reinitialize logger") {
		t.Errorf("runtime did not preserve log file error: %v", err)
	}
	if Log() != oldLogger || logCurrentConf != oldConf {
		t.Error("failed reload changed the active logger or cached configuration")
	}
	if err := os.Remove(parent); err != nil {
		t.Fatalf("remove log directory blocker: %v", err)
	}
	if err := (&M{}).Runtime(); err != nil {
		t.Fatalf("retry unchanged logger configuration: %v", err)
	}
	if Log() == oldLogger || logCurrentConf != cfg.LogConf {
		t.Error("retry did not publish the repaired logger")
	}
	Log().Info().Msg("Logger retry succeeded")
	body, err := os.ReadFile(cfg.LogConf.File)
	if err != nil || !strings.Contains(string(body), "Logger retry succeeded") {
		t.Fatalf("read repaired log file: %v, body=%s", err, body)
	}
}

// TestAlarmSwitchConcurrentReload 验证 Runtime 激活报警开关时与 writer 读取没有数据竞争.
// 关闭 writer 投递, 只覆盖配置开关的并发边界, 不创建不必要的后台 HTTP 任务.
func TestAlarmSwitchConcurrentReload(t *testing.T) {
	cfg := prepareCommonConfig(t)
	installCommonTestLoggers(io.Discard, zerolog.TraceLevel)
	logCurrentConf = cfg.LogConf
	writer := newAlarmWriter(zerolog.WarnLevel)
	writer.off.Store(true)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 1000 {
			_, _ = writer.Write([]byte(`{"M":"Concurrent alarm"}`))
		}
	})
	for i := range 1000 {
		config.AlarmOn.Store(i%2 == 0)
		if err := loadLogger(); err != nil {
			t.Errorf("reload alarm switch: %v", err)
		}
	}
	wg.Wait()
}

// TestAlarmSwitchRuntimeActivation 验证配置开关只在日志 Runtime 激活后影响新报警任务.
func TestAlarmSwitchRuntimeActivation(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	installCommonTestLoggers(io.Discard, zerolog.TraceLevel)
	logCurrentConf = cfg.LogConf
	pool := &queuedLogPool{Pooler: commonTestState.helperPool}
	previousPool := ants.SwapDefaultAntsPool(pool)
	t.Cleanup(func() { ants.SwapDefaultAntsPool(previousPool) })
	writer := newAlarmWriter(zerolog.WarnLevel)
	for _, tc := range []struct {
		enabled bool
		before  int
		after   int
	}{{true, 0, 1}, {false, 1, 0}} {
		before := len(pool.tasks)
		config.AlarmOn.Store(tc.enabled)
		_, _ = writer.Write(nil)
		if got := len(pool.tasks) - before; got != tc.before {
			t.Fatalf("configuration change activated alarm before Runtime: tasks=%d", got)
		}
		if err := loadLogger(); err != nil {
			t.Fatalf("activate alarm switch: %v", err)
		}
		before = len(pool.tasks)
		_, _ = writer.Write(nil)
		if got := len(pool.tasks) - before; got != tc.after {
			t.Fatalf("Runtime did not activate alarm switch: enabled=%v, tasks=%d", tc.enabled, got)
		}
	}
}

// TestPostLogCapturesConfiguredURL 验证无需启动 sender 即可投递, 每次异步任务固定提交时的 URL.
func TestPostLogCapturesConfiguredURL(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	req.SetCommonRetryCount(0)
	bodiesA, bodiesB := make(chan string, 2), make(chan string, 2)
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodiesA <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodiesB <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(serverB.Close)
	pool := &queuedLogPool{Pooler: commonTestState.helperPool}
	previousPool := ants.SwapDefaultAntsPool(pool)
	t.Cleanup(func() { ants.SwapDefaultAntsPool(previousPool) })

	cfg.LogConf.PostAPI = serverA.URL
	PostLog([]byte(`{"kind":"immediate"}`))
	cfg.LogConf.PostAPI = serverB.URL
	bb := buffer.Get()
	_, _ = bb.Write([]byte(`,{"kind":"batch"}`))
	postLog(bb)
	// 修改发生在任务执行前, 不与 Config() 的同步读取并发.
	cfg.LogConf.PostAPI = ""
	if len(pool.tasks) != 2 {
		t.Fatalf("configured URL submitted %d tasks, want 2", len(pool.tasks))
	}
	for _, task := range pool.tasks {
		task()
	}
	assertCommonHTTPBody(t, bodiesA, `{"kind":"immediate"}`)
	assertCommonHTTPBody(t, bodiesB, `[{"kind":"batch"}]`)
}

// queuedLogPool 延迟执行当前测试同步提交的任务, 使配置切换时序确定且不依赖调度速度.
type queuedLogPool struct {
	ants.Pooler
	tasks []func()
}

// Submit 只保存任务; 用例在修改配置后自行执行, 其他 pool 行为仍由测试 pool 提供.
func (p *queuedLogPool) Submit(task func()) error {
	p.tasks = append(p.tasks, task)
	return nil
}
