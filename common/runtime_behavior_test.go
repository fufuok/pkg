package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fufuok/ants"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// TestDefaultPoolPanicAndChannelContract 验证 pool panic handler 与两类无限缓冲 channel 的终止语义.
func TestDefaultPoolPanicAndChannelContract(t *testing.T) {
	prepareCommonConfig(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	MaxGoPool = config.DefaultGOMAXPROCS * 4
	pool, err := newDefaultPool()
	if err != nil {
		t.Fatalf("create default pool: %v", err)
	}
	t.Cleanup(pool.Release)
	if pool.Cap() <= 0 {
		t.Fatalf("default pool capacity = %d", pool.Cap())
	}
	if err := pool.Submit(func() { panic("pool panic") }); err != nil {
		t.Fatalf("submit panic task: %v", err)
	}
	waitCommonCondition(t, time.Second, "pool panic handler", func() bool {
		return strings.Contains(logs.String(), "Recovery worker: pool panic")
	})

	plain := NewChanx[int](8)
	closePlain := sync.OnceFunc(func() { close(plain.In) })
	t.Cleanup(closePlain)
	plain.In <- 7
	select {
	case got := <-plain.Out:
		if got != 7 {
			t.Fatalf("plain channel value = %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("plain channel did not forward a value")
	}
	closePlain()
	select {
	case _, ok := <-plain.Out:
		if ok {
			t.Fatal("plain channel remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("plain channel did not close")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	withContext := NewChanxWithContext[int](ctx, 8)
	withContext.In <- 9
	select {
	case got := <-withContext.Out:
		if got != 9 {
			t.Fatalf("context channel value = %d", got)
		}
	case <-time.After(time.Second):
		t.Fatal("context channel did not forward a value")
	}
	cancel()
	select {
	case _, ok := <-withContext.Out:
		if ok {
			t.Fatal("context channel remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("context channel did not close")
	}
}

// TestSetClockOffsetBoundary 验证小偏差保持原值, 大偏差附加调整量后发布.
func TestSetClockOffsetBoundary(t *testing.T) {
	preserveCommonPackageState(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.TraceLevel)
	ClockOffsetLimit = float64(10 * time.Millisecond)
	ClockOffsetAdjust = int64(time.Millisecond)
	clockOffset.Store(0)
	SetClockOffset(5 * time.Millisecond)
	if got := GetClockOffset(); got != 0 {
		t.Fatalf("small clock offset = %s", got)
	}
	SetClockOffset(20 * time.Millisecond)
	if got := GetClockOffset(); got != 21*time.Millisecond {
		t.Fatalf("adjusted clock offset = %s", got)
	}
	if !strings.Contains(logs.String(), "NTP sync completed") {
		t.Fatal("clock offset update was not logged")
	}
}

// TestCommonProductionLifecycleSubprocess 在独立进程验证完整 Start/Stop, 避免污染父进程资源.
func TestCommonProductionLifecycleSubprocess(t *testing.T) {
	tests := []struct {
		name     string
		scenario string
		want     string
	}{
		{name: "lookup", scenario: "lookup", want: "internal=192.0.2.10 external=203.0.113.9"},
		{name: "fallback", scenario: "fallback", want: "internal=192.0.2.10 external=198.51.100.8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runCommonSubprocess(t, "TestCommonProductionLifecycleSubprocessHelper", "PKG_COMMON_LIFECYCLE_HELPER", tc.scenario)
			if err != nil {
				t.Fatalf("common lifecycle helper: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), "PKG_COMMON_LIFECYCLE_OK scenario="+tc.scenario+" "+tc.want) {
				t.Fatalf("common lifecycle marker missing: %s", output)
			}
		})
	}
}

// TestCommonProductionLifecycleSubprocessHelper 使用包内查询缝隔离内外网请求并执行生产生命周期.
func TestCommonProductionLifecycleSubprocessHelper(t *testing.T) {
	scenario := os.Getenv("PKG_COMMON_LIFECYCLE_HELPER")
	if scenario == "" {
		return
	}
	internalIPv4Lookup = func() string { return "192.0.2.10" }
	wantExternal := "203.0.113.9"
	switch scenario {
	case "lookup":
		externalIPv4Lookup = func() string { return wantExternal }
	case "fallback":
		wantExternal = "198.51.100.8"
		externalIPv4Lookup = func() string { return "" }
	default:
		t.Fatalf("unknown common lifecycle scenario %q", scenario)
	}

	config.Debug = true
	config.InitTester()
	defer config.StopTester()
	cfg := config.Config()
	cfg.LogConf.PostBatchNum = 10
	cfg.LogConf.PostBatchBytes = 1024
	cfg.LogConf.PostIntervalDuration = time.Second
	cfg.SYSConf.ReqTimeoutDuration = 50 * time.Millisecond
	cfg.NodeConf.NodeInfo.NodeIP = "198.51.100.8"

	m := &M{}
	stopped := false
	defer func() {
		if !stopped {
			_ = m.Stop()
		}
	}()
	if err := m.Start(); err != nil {
		t.Fatalf("start common lifecycle: %v", err)
	}
	if ReqUpload == nil || ReqDownload == nil || LogChan == nil {
		t.Fatal("common start did not initialize runtime components")
	}
	done := make(chan struct{})
	if err := ants.Submit(func() { close(done) }); err != nil {
		t.Fatalf("submit to production pool: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("production pool did not execute a task")
	}
	waitCommonCondition(t, time.Second, "stubbed server IPs", func() bool {
		return InternalIPv4 == "192.0.2.10" && ExternalIPv4 == wantExternal
	})

	var exitLog lockedBuffer
	installCommonTestLoggers(&exitLog, zerolog.TraceLevel)
	if err := m.Stop(); err != nil {
		t.Fatalf("stop common lifecycle: %v", err)
	}
	stopped = true
	waitCommonCondition(t, time.Second, "log sender exit", func() bool {
		return strings.Contains(exitLog.String(), "Log sender exited")
	})
	if err := ants.Submit(func() {}); err == nil {
		t.Fatal("production pool accepted a task after Stop")
	}
	fmt.Printf("PKG_COMMON_LIFECYCLE_OK scenario=%s internal=%s external=%s\n", scenario, InternalIPv4, ExternalIPv4)
}

// runCommonSubprocess 在限定时间内执行一个 helper, 并区分超时和正常错误退出.
func runCommonSubprocess(t *testing.T, testName, envName, envValue string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), envName+"="+envValue)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("common helper %s exceeded 5s: %s", testName, output)
	}
	return output, err
}
