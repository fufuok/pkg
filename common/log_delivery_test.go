package common

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fufuok/ants"
	"github.com/fufuok/bytespool/buffer"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"
)

// rejectingLogPool 固定拒绝任务, 使池关闭或饱和分支不依赖并发调度.
type rejectingLogPool struct {
	ants.Pooler
}

// Submit 返回固定错误, 不取得任务或缓冲区的所有权.
func (p *rejectingLogPool) Submit(func()) error {
	return errors.New("fixture rejected log delivery")
}

// TestPostLogRejectedIsVisible 验证投递被拒绝时记录本地诊断并安全回收缓冲.
func TestPostLogRejectedIsVisible(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	cfg.LogConf.PostAPI = "http://127.0.0.1:1"
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	previous := ants.SwapDefaultAntsPool(&rejectingLogPool{Pooler: commonTestState.helperPool})
	t.Cleanup(func() { ants.SwapDefaultAntsPool(previous) })

	bb := buffer.Get()
	_, _ = bb.WriteString(`,{"kind":"batch"}`)
	postLog(bb)
	PostLog([]byte(`{"kind":"immediate"}`))
	if got := strings.Count(logs.String(), "Submitting log"); got != 2 {
		t.Fatalf("rejected submissions produced %d diagnostics, want 2: %s", got, logs.String())
	}
}

// TestSubmitLogOwnership 验证正常、HTTP 非 2xx 和提交拒绝各自只归还一次资源, 且非 2xx 会写本地采样诊断.
func TestSubmitLogOwnership(t *testing.T) {
	prepareCommonRuntime(t)
	req.SetCommonRetryCount(0)
	for _, status := range []int{http.StatusNoContent, http.StatusServiceUnavailable} {
		var logs lockedBuffer
		installCommonTestLoggers(&logs, zerolog.TraceLevel)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		pool := &queuedLogPool{Pooler: commonTestState.helperPool}
		previous := ants.SwapDefaultAntsPool(pool)
		releases := 0
		submitLog(server.URL, []byte(`{"kind":"owned"}`), func() { releases++ })
		if releases != 0 || len(pool.tasks) != 1 {
			t.Fatal("submission did not transfer ownership to queued task")
		}
		pool.tasks[0]()
		ants.SwapDefaultAntsPool(previous)
		server.Close()
		if releases != 1 {
			t.Fatalf("delivery did not finish exactly once: releases=%d", releases)
		}
		hasWarn := strings.Contains(logs.String(), "Posting log")
		if status == http.StatusNoContent && hasWarn {
			t.Fatalf("successful HTTP delivery unexpectedly logged warning: %s", logs.String())
		}
		if status == http.StatusServiceUnavailable && !hasWarn {
			t.Fatalf("HTTP error status did not log warning: %s", logs.String())
		}
	}
	previous := ants.SwapDefaultAntsPool(&rejectingLogPool{Pooler: commonTestState.helperPool})
	t.Cleanup(func() { ants.SwapDefaultAntsPool(previous) })
	releases := 0
	submitLog("http://127.0.0.1:1", nil, func() { releases++ })
	if releases != 1 {
		t.Fatalf("rejected submission returned resources %d times, want 1", releases)
	}
}
