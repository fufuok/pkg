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
	"github.com/fufuok/bytespool/buffer"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"
)

// TestLogSenderBatchFlushAndFailure 验证条数、字节、定时、立即发送和 HTTP 失败分支.
func TestLogSenderBatchFlushAndFailure(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	submissions := installCommonSubmissionCounter(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	req.SetCommonRetryCount(0)

	bodies := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg.LogConf.PostAPI = server.URL

	cfg.LogConf.PostBatchNum = 2
	cfg.LogConf.PostBatchBytes = 1 << 20
	cfg.LogConf.PostIntervalDuration = time.Second
	stopSender := startCommonLogSender(t)
	LogChan.In <- []byte(`{"n":1}`)
	LogChan.In <- []byte(`{"n":2}`)
	assertCommonHTTPBody(t, bodies, `[{"n":1},{"n":2}]`)
	stopSender()

	cfg.LogConf.PostBatchNum = 100
	cfg.LogConf.PostBatchBytes = 5
	cfg.LogConf.PostIntervalDuration = time.Second
	stopSender = startCommonLogSender(t)
	LogChan.In <- []byte(`{"kind":"bytes"}`)
	assertCommonHTTPBody(t, bodies, `[{"kind":"bytes"}]`)
	stopSender()

	cfg.LogConf.PostBatchNum = 100
	cfg.LogConf.PostBatchBytes = 1 << 20
	cfg.LogConf.PostIntervalDuration = 10 * time.Millisecond
	stopSender = startCommonLogSender(t)
	LogChan.In <- []byte(`{"kind":"timer"}`)
	assertCommonHTTPBody(t, bodies, `[{"kind":"timer"}]`)
	stopSender()

	postAPI = server.URL
	PostLog([]byte(`{"kind":"immediate"}`))
	assertCommonHTTPBody(t, bodies, `{"kind":"immediate"}`)

	postAPI = ""
	beforeEmptyAPI := submissions.Load()
	bb := buffer.Get()
	_, _ = bb.Write([]byte(`,{"kind":"discarded"}`))
	postLog(bb)
	PostLog([]byte(`{"kind":"discarded-immediate"}`))
	if got := submissions.Load(); got != beforeEmptyAPI {
		t.Fatalf("empty post API submitted %d tasks", got-beforeEmptyAPI)
	}
	if strings.Contains(logs.String(), "Posting log") {
		t.Fatal("empty post API produced a request failure log")
	}

	failedRequests := make(chan struct{}, 1)
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failedRequests <- struct{}{}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(failingServer.Close)
	postAPI = failingServer.URL
	PostLog([]byte(`{"kind":"failure"}`))
	select {
	case <-failedRequests:
	case <-time.After(time.Second):
		t.Fatal("failure fixture did not receive the request")
	}
	waitCommonCondition(t, time.Second, "log sender failure message", func() bool {
		return strings.Contains(logs.String(), "Posting log")
	})
}

// startCommonLogSender 为单个场景建立独立队列, 并立即注册失败路径清理.
func startCommonLogSender(t *testing.T) func() {
	t.Helper()
	queue := NewChanx[[]byte](16)
	LogChan = queue
	done := make(chan struct{})
	go func() {
		logSender()
		close(done)
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			close(queue.In)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("log sender did not stop after closing input")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// submissionCountingPool 复用真实 pool, 并记录同步提交次数.
type submissionCountingPool struct {
	ants.Pooler
	submissions atomic.Int64
}

// Submit 记录一次提交后转发给真实 pool.
func (p *submissionCountingPool) Submit(task func()) error {
	p.submissions.Add(1)
	return p.Pooler.Submit(task)
}

// Load 返回当前累计提交次数.
func (p *submissionCountingPool) Load() int64 {
	return p.submissions.Load()
}

// installCommonSubmissionCounter 安装只观测提交次数的 pool 包装, cleanup 恢复助手 pool.
func installCommonSubmissionCounter(t *testing.T) *submissionCountingPool {
	t.Helper()
	if commonTestState == nil || commonTestState.helperPool == nil {
		t.Fatal("common test runtime is not initialized")
	}
	helpPool := commonTestState.helperPool
	counter := &submissionCountingPool{Pooler: helpPool}
	if previous := ants.SwapDefaultAntsPool(counter); previous != helpPool {
		ants.SwapDefaultAntsPool(previous)
		t.Fatal("submission counter did not replace the common helper pool")
	}
	t.Cleanup(func() {
		if current := ants.SwapDefaultAntsPool(helpPool); current != counter {
			t.Errorf("submission counter cleanup displaced an unexpected pool")
		}
	})
	return counter
}
