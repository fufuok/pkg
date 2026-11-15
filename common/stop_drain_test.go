package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fufuok/ants"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"
)

// countingReleasePool 记录被拒绝的提交, 其余行为委托真实池.
type countingReleasePool struct {
	ants.Pooler
	rejected atomic.Int32
}

func (p *countingReleasePool) Submit(task func()) error {
	err := p.Pooler.Submit(task)
	if err != nil {
		p.rejected.Add(1)
	}
	return err
}

// installStopPool 安装一个由测试拥有的真实默认池 (Stop 会关闭它), cleanup 换回助手池.
func installStopPool(t *testing.T) *countingReleasePool {
	t.Helper()
	pool, err := newDefaultPool()
	if err != nil {
		t.Fatal(err)
	}
	p := &countingReleasePool{Pooler: pool}
	previous := ants.SwapDefaultAntsPool(p)
	t.Cleanup(func() {
		ants.SwapDefaultAntsPool(previous)
		pool.Release()
	})
	return p
}

// TestStopDeliversTailLogBatch 验证 Stop 返回前已排空日志队列并完成末批发送:
// sender 晚于 Stop 开始执行时, 末批也不会被已关闭的协程池拒绝; 发送较慢时 Stop 等待其完成.
func TestStopDeliversTailLogBatch(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	req.SetCommonRetryCount(0)
	var (
		mu       sync.Mutex
		received []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// 慢响应: 默认池 Release 等待在途发送任务, Stop 返回时应已看到记录.
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg.LogConf.PostAPI = server.URL
	cfg.LogConf.PostBatchNum = 100
	cfg.LogConf.PostBatchBytes = 1 << 20
	cfg.LogConf.PostIntervalDuration = time.Hour
	pool := installStopPool(t)
	// 本用例依赖 Stop 等到延后启动的 sender 完成, 等待上限设长, 不受生产默认值和机器负载影响.
	old := stopDrainTimeout
	stopDrainTimeout = time.Minute
	t.Cleanup(func() { stopDrainTimeout = old })

	LogChan = NewChanx[[]byte](16)
	done := make(chan struct{})
	logSenderDone = done
	LogChan.In <- []byte(`{"accepted":true}`)
	// sender 延后启动, 固定 "Stop 已关闭队列而 sender 尚未提交末批" 的调度顺序.
	go func() {
		defer close(done)
		time.Sleep(20 * time.Millisecond)
		logSender()
	}()
	if err := (&M{}).Stop(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	got := strings.Join(received, "|")
	mu.Unlock()
	if pool.rejected.Load() != 0 || got != `[{"accepted":true}]` {
		t.Fatalf("tail batch not delivered before Stop returned: rejected=%d received=%q logs=%s",
			pool.rejected.Load(), got, logs.String())
	}
}

// TestStopDrainTimeoutBounded 验证 sender 卡住时 Stop 在上限内返回并记录告警, 不返回错误.
func TestStopDrainTimeoutBounded(t *testing.T) {
	prepareCommonRuntime(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	installStopPool(t)
	old := stopDrainTimeout
	stopDrainTimeout = 50 * time.Millisecond
	t.Cleanup(func() { stopDrainTimeout = old })

	LogChan = NewChanx[[]byte](16)
	logSenderDone = make(chan struct{}) // 永不关闭: 模拟 sender 卡住
	start := time.Now()
	if err := (&M{}).Stop(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Stop took %v, want bounded by drain timeout", d)
	}
	if !strings.Contains(logs.String(), "Log sender drain timed out") {
		t.Fatalf("missing drain timeout warning: %s", logs.String())
	}
}

// TestStopDeliversBatchHeldBySender 覆盖 sender 已在运行且末批停在内存缓冲 (未到条数/字节/周期阈值) 的真实分支:
// Out 关闭后 select 立即就绪, 不必等待 PostIntervalDuration, 末批在 Stop 返回前送达.
func TestStopDeliversBatchHeldBySender(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	req.SetCommonRetryCount(0)
	var (
		mu       sync.Mutex
		received []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cfg.LogConf.PostAPI = server.URL
	cfg.LogConf.PostBatchNum = 100
	cfg.LogConf.PostBatchBytes = 1 << 20
	cfg.LogConf.PostIntervalDuration = time.Hour
	installStopPool(t)

	initLogSender()
	LogChan.In <- []byte(`{"n":1}`)
	LogChan.In <- []byte(`{"n":2}`)
	// 等 sender 取走两条 (Len 统计 In/Out/缓冲), 此后它阻塞在 select 上, 末批只在内存缓冲里.
	waitCommonCondition(t, time.Second, "sender consumed queue", func() bool { return LogChan.Len() == 0 })
	start := time.Now()
	if err := (&M{}).Stop(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Stop took %v, sender waited for ticker", d)
	}
	mu.Lock()
	got := strings.Join(received, "|")
	mu.Unlock()
	if got != `[{"n":1},{"n":2}]` {
		t.Fatalf("held batch not delivered before Stop returned: %q", got)
	}
}

// TestStopTailPostBounded 验证日志接口慢而未断时, 停机末批发送受 logTailPostTimeout 限制,
// Stop (含默认池 Release 等待在途发送) 不会被拖到接口响应为止.
func TestStopTailPostBounded(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	req.SetCommonRetryCount(0)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	defer close(release)
	cfg.LogConf.PostAPI = server.URL
	cfg.LogConf.PostBatchNum = 100
	cfg.LogConf.PostBatchBytes = 1 << 20
	cfg.LogConf.PostIntervalDuration = time.Hour
	installStopPool(t)
	old := logTailPostTimeout
	logTailPostTimeout = 100 * time.Millisecond
	t.Cleanup(func() { logTailPostTimeout = old })

	initLogSender()
	LogChan.In <- []byte(`{"n":1}`)
	start := time.Now()
	if err := (&M{}).Stop(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Stop took %v with a slow log API, tail post is not bounded", d)
	}
	if !strings.Contains(logs.String(), "Posting log") {
		t.Fatalf("missing tail post timeout diagnostic: %s", logs.String())
	}
}
