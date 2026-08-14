package utils

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

func TestSafeGo(t *testing.T) {
	type recoveryResult struct {
		err   any
		trace []byte
	}

	// 每个异步入口使用独立结果通道, 既建立内存同步关系, 也避免固定 Sleep 掩盖未执行回调.
	assertRecovery := func(start func(RecoveryCallback)) {
		t.Helper()
		result := make(chan recoveryResult, 1)
		start(func(err any, trace []byte) {
			result <- recoveryResult{err: err, trace: trace}
		})

		select {
		case got := <-result:
			assert.Equal(t, "fn1", got.err)
			assert.Equal(t, true, bytes.Contains(got.trace, []byte("panic")))
		case <-time.After(time.Second):
			t.Fatal("recovery callback timed out")
		}
	}

	ctx := context.Background()
	assertRecovery(func(cb RecoveryCallback) { SafeGo(testFn2, cb) })
	assertRecovery(func(cb RecoveryCallback) { SafeGoWithContext(ctx, testFn3, cb) })
	assertRecovery(func(cb RecoveryCallback) { SafeGoCommonFunc(ctx, testFn4, cb) })
}

var (
	testFn1 = func() {
		panic("fn1")
	}
	testFn2 = func() {
		testFn1()
	}
	testFn3 = func(ctx context.Context) {
		testFn1()
	}
	testFn4 = func(args any) {
		testFn1()
	}
)
