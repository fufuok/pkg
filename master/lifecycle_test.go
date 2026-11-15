package master

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/utils"
)

// gatePipeline 在 Runtime 中握手暂停, 记录 Stop 与 Runtime 的交错关系.
type gatePipeline struct {
	entered       chan struct{}
	resume        chan struct{}
	stopCalls     atomic.Int32
	usedAfterStop atomic.Bool
	// onRuntime 非 nil 时在 Runtime 中调用 (模拟 Runtime 内重入 Stop).
	onRuntime func()
}

func (p *gatePipeline) Start() error { return nil }

// Runtime 通知已进入后等待放行, 恢复时若业务已被 Stop 则记为使用了已关闭的资源.
func (p *gatePipeline) Runtime() error {
	if p.onRuntime != nil {
		p.onRuntime()
		return nil
	}
	close(p.entered)
	<-p.resume
	p.usedAfterStop.Store(p.stopCalls.Load() > 0)
	return nil
}

func (p *gatePipeline) Stop() error {
	p.stopCalls.Add(1)
	return nil
}

// runGateCycle 在新 goroutine 中按 watcher 的方式执行一个受闸门约束的 Runtime 周期, 返回周期结束信号.
func runGateCycle(t *testing.T, g *reloadGate) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		owner, _ := utils.GoroutineID()
		if !g.begin(owner) {
			t.Error("begin should succeed before Stop")
			return
		}
		defer g.end()
		runtimePipeline()
	}()
	return done
}

// pinStopWait 把在途周期等待上限设为 d 并在测试结束时恢复. 依赖 "Stop 一直等到 Runtime 放行" 的用例设为很长,
// 不受生产默认值 (2 秒) 和机器负载影响; 验证超时路径的用例设为很短.
func pinStopWait(t *testing.T, d time.Duration) {
	t.Helper()
	old := stopWaitTimeout
	stopWaitTimeout = d
	t.Cleanup(func() { stopWaitTimeout = old })
}

// waitCondition 轮询等待 cond 为真, 超时则失败.
func waitCondition(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitClosed 等待 ch 关闭, 超时则失败.
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

// TestStopWaitsInFlightRuntime 验证 Stop 等待在途 Runtime 结束后才关闭业务, 且停机后不再开始新周期.
func TestStopWaitsInFlightRuntime(t *testing.T) {
	preserveMasterPackageState(t)
	pinStopWait(t, time.Minute)
	g := newReloadGate()
	currentGate.Store(g)
	p := &gatePipeline{entered: make(chan struct{}), resume: make(chan struct{})}
	mainPipelines = []Pipeline{p}

	cycle := runGateCycle(t, g)
	waitClosed(t, p.entered, "runtime entry")
	stopped := make(chan struct{})
	go func() {
		Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned while Runtime was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	if p.stopCalls.Load() != 0 {
		t.Fatal("pipeline Stop ran concurrently with in-flight Runtime")
	}
	select {
	case <-g.quit:
		t.Fatal("quit closed before in-flight cycle finished")
	default:
	}

	close(p.resume)
	waitClosed(t, cycle, "runtime cycle")
	waitClosed(t, stopped, "Stop")
	if p.usedAfterStop.Load() {
		t.Fatal("Runtime observed resources closed by Stop")
	}
	if p.stopCalls.Load() != 1 {
		t.Fatalf("pipeline Stop calls = %d, want 1", p.stopCalls.Load())
	}
	waitClosed(t, g.quit, "quit")
	if g.begin(1) {
		t.Fatal("begin must fail after Stop")
	}
}

// TestStopReentrantFromRuntime 验证 Runtime 内同步调用 Stop 不会自等待死锁, 本周期其余模块的 Runtime 跳过
// (它们已被关闭), 周期结束时才关闭 quit; 随后 Run 的 defer Stop 不再重复清理.
func TestStopReentrantFromRuntime(t *testing.T) {
	preserveMasterPackageState(t)
	g := newReloadGate()
	currentGate.Store(g)
	p := &gatePipeline{}
	later := &gatePipeline{onRuntime: func() { t.Error("Runtime after reentrant Stop must be skipped") }}
	quitBeforeEnd := true
	p.onRuntime = func() {
		Stop()
		select {
		case <-g.quit:
		default:
			quitBeforeEnd = false
		}
	}
	mainPipelines = []Pipeline{p, later}

	waitClosed(t, runGateCycle(t, g), "reentrant runtime cycle")
	if quitBeforeEnd {
		t.Fatal("quit must stay open until the reentrant cycle ends")
	}
	waitClosed(t, g.quit, "quit")
	Stop()
	if p.stopCalls.Load() != 1 || later.stopCalls.Load() != 1 {
		t.Fatalf("pipeline Stop calls = %d/%d, want 1/1", p.stopCalls.Load(), later.stopCalls.Load())
	}
}

// TestSecondStopWaitsAndCleansOnce 验证并发的第二次 Stop 同样等待在途 Runtime, 业务清理只执行一次.
func TestSecondStopWaitsAndCleansOnce(t *testing.T) {
	preserveMasterPackageState(t)
	pinStopWait(t, time.Minute)
	g := newReloadGate()
	currentGate.Store(g)
	p := &gatePipeline{entered: make(chan struct{}), resume: make(chan struct{})}
	mainPipelines = []Pipeline{p}

	cycle := runGateCycle(t, g)
	waitClosed(t, p.entered, "runtime entry")
	first, second := make(chan struct{}), make(chan struct{})
	go func() {
		Stop()
		close(first)
	}()
	// 等第一次 Stop 已标记停机, 第二次 Stop 走 "已停机" 分支.
	waitCondition(t, g.stopping, "first Stop requested")
	go func() {
		Stop()
		close(second)
	}()
	select {
	case <-first:
		t.Fatal("first Stop returned while Runtime was in flight")
	case <-second:
		t.Fatal("second Stop bypassed the wait for in-flight Runtime")
	case <-time.After(50 * time.Millisecond):
	}
	if p.stopCalls.Load() != 0 {
		t.Fatal("pipeline Stop ran concurrently with in-flight Runtime")
	}

	close(p.resume)
	waitClosed(t, cycle, "runtime cycle")
	waitClosed(t, first, "first Stop")
	waitClosed(t, second, "second Stop")
	if p.usedAfterStop.Load() {
		t.Fatal("Runtime observed resources closed by Stop")
	}
	if p.stopCalls.Load() != 1 {
		t.Fatalf("pipeline Stop calls = %d, want 1", p.stopCalls.Load())
	}
}

// TestStopWaitBoundedForHungRuntime 验证 Runtime 重入 Stop 后卡住不返回时, 后续 Stop 在等待上限后返回并关闭 quit,
// 不重复清理; 卡住的周期恢复后安全结束 (quit 不会重复关闭).
func TestStopWaitBoundedForHungRuntime(t *testing.T) {
	preserveMasterPackageState(t)
	pinStopWait(t, 50*time.Millisecond)
	g := newReloadGate()
	currentGate.Store(g)
	resume := make(chan struct{})
	reentered := make(chan struct{})
	p := &gatePipeline{}
	p.onRuntime = func() {
		Stop()
		close(reentered)
		<-resume
	}
	mainPipelines = []Pipeline{p}

	cycle := runGateCycle(t, g)
	waitClosed(t, reentered, "reentrant Stop")
	start := time.Now()
	Stop()
	if d := time.Since(start); d < stopWaitTimeout || d > 2*time.Second {
		t.Fatalf("Stop returned after %v, want bounded by %v", d, stopWaitTimeout)
	}
	waitClosed(t, g.quit, "quit")
	if p.stopCalls.Load() != 1 {
		t.Fatalf("pipeline Stop calls = %d, want 1", p.stopCalls.Load())
	}
	close(resume)
	waitClosed(t, cycle, "hung runtime cycle")
}

// TestStopIdleGateAndWatcherExit 验证无在途周期时 Stop 立即关闭 quit, 重复 Stop 不阻塞, watcher 随之退出.
func TestStopIdleGateAndWatcherExit(t *testing.T) {
	prepareMasterConfig(t)
	g := newReloadGate()
	currentGate.Store(g)
	Stop()
	waitClosed(t, g.quit, "quit")
	Stop()
	if g.begin(1) {
		t.Fatal("begin must fail after Stop")
	}

	exited := make(chan struct{})
	go func() {
		startWatcher(g)
		close(exited)
	}()
	waitClosed(t, exited, "watcher")
}

// runtimeWaitPipeline 的 Stop 等待在途 Runtime 退出后才完成清理 (模拟业务 Stop 与 Runtime 共用资源锁).
type runtimeWaitPipeline struct {
	runtimeExited <-chan struct{}
}

func (p *runtimeWaitPipeline) Start() error   { return nil }
func (p *runtimeWaitPipeline) Runtime() error { return nil }

func (p *runtimeWaitPipeline) Stop() error {
	<-p.runtimeExited
	return nil
}

// TestReentrantStopAfterExternalCleanupClaimed 验证外部 Stop 等待超时并认领清理后, Runtime 恢复时重入 Stop
// 不再等待该清理 (清理中的 Pipeline.Stop 正等 Runtime 退出), 双方都能结束.
func TestReentrantStopAfterExternalCleanupClaimed(t *testing.T) {
	preserveMasterPackageState(t)
	pinStopWait(t, 50*time.Millisecond)
	g := newReloadGate()
	currentGate.Store(g)
	entered, resume, runtimeExited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := &gatePipeline{}
	p.onRuntime = func() {
		close(entered)
		<-resume
		Stop()
		close(runtimeExited)
	}
	mainPipelines = []Pipeline{p, &runtimeWaitPipeline{runtimeExited: runtimeExited}}

	cycle := runGateCycle(t, g)
	waitClosed(t, entered, "runtime entry")
	external := make(chan struct{})
	go func() {
		Stop()
		close(external)
	}()
	// 外部 Stop 等待超时后认领清理, 清理阻塞在 runtimeWaitPipeline.Stop 上.
	waitCondition(t, func() bool {
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.cleaned != nil
	}, "external cleanup claimed")
	close(resume)
	waitClosed(t, runtimeExited, "reentrant Stop inside Runtime")
	waitClosed(t, cycle, "runtime cycle")
	waitClosed(t, external, "external Stop")
}

// TestWatcherSignalsAbandonedAfterQuit 验证 scheduler 已退出 (quit 关闭) 时, watcher 周期末尾的重载信号
// 与重启信号不会永久阻塞在无接收者的无缓冲 channel 上.
func TestWatcherSignalsAbandonedAfterQuit(t *testing.T) {
	prepareMasterConfig(t)
	config.AppConfigBody = nil
	g := newReloadGate()
	// 只关闭 quit 不标记停机: 模拟最后一次停机检查之后 Stop 等待超时, scheduler 先行退出.
	g.closeQuit()
	currentGate.Store(g)
	reloadChan, restartChan = make(chan bool), make(chan bool)

	// 走完整周期到末尾的重载通知: 二进制 (独立文件) 未变, 配置有变化且加载成功, 业务 Runtime 执行过.
	// mainFile 不能复用配置文件, 否则写配置会被当作二进制变化而提前走重启分支, 到不了末尾的重载发送.
	mainFile = filepath.Join(t.TempDir(), "main.bin")
	if err := os.WriteFile(mainFile, []byte("bin"), 0o600); err != nil {
		t.Fatal(err)
	}
	watcherMD5.Store(MainWatcherKey, MD5Files(mainFile))
	body := []byte(`{"sys_conf":{"watcher_interval":"30s","req_timeout":"1s"}}`)
	if err := os.WriteFile(config.ConfigFile, body, 0o600); err != nil {
		t.Fatal(err)
	}
	watcherMD5.Store(MainWatcherConfKey, "different")
	var reloaded atomic.Bool
	mainPipelines = []Pipeline{&gatePipeline{onRuntime: func() { reloaded.Store(true) }}}
	interval := time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchOnce(g, ticker, &interval)
	}()
	waitClosed(t, done, "watchOnce reload notification")
	if !reloaded.Load() {
		t.Fatal("cycle returned before the business Runtime, final reload send not exercised")
	}

	restarted := make(chan struct{})
	go func() {
		defer close(restarted)
		checkUpgradeOrRestart(config.SYSConf{RestartMain: true})
		handleBinaryChange(binaryChangeRestart)
	}()
	waitClosed(t, restarted, "restart notifications")
}

// TestSchedulerExitsAndCancelsRemoteOnStop 以真实 mainScheduler -> initWatcher -> startWatcher 接线验证:
// Stop 后 scheduler 退出, 远端拉取任务收到取消, watcher 不再调度应用监控器.
// 先等 watcher 跑过一个真实周期再 Stop (周期内的全局读取都在 Stop 等待范围内), 不替换全局 logger, 避免测试收尾竞态.
func TestSchedulerExitsAndCancelsRemoteOnStop(t *testing.T) {
	cfg := prepareMasterConfig(t)
	cfg.SYSConf.WatcherIntervalDuration = 5 * time.Millisecond
	pinStopWait(t, time.Minute)
	reloadChan, restartChan = make(chan bool), make(chan bool)

	remoteStarted, remoteExited := make(chan struct{}), make(chan struct{})
	remotePipelines = []ContextFunc{func(ctx context.Context) {
		close(remoteStarted)
		<-ctx.Done()
		close(remoteExited)
	}}
	var ticks atomic.Int64
	cycled := make(chan struct{})
	var once sync.Once
	Watcher{Key: "lifecycle-probe", Always: true, Func: func() {
		ticks.Add(1)
		once.Do(func() { close(cycled) })
	}}.Start()

	g := newReloadGate()
	currentGate.Store(g)
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		mainScheduler(g)
	}()
	waitClosed(t, remoteStarted, "remote pipeline start")
	waitClosed(t, cycled, "first watcher cycle")

	Stop()
	waitClosed(t, schedulerDone, "scheduler exit")
	waitClosed(t, remoteExited, "remote context cancellation")
	waitClosed(t, g.quit, "quit")
	n := ticks.Load()
	time.Sleep(50 * time.Millisecond)
	if got := ticks.Load(); got != n {
		t.Fatalf("watcher kept scheduling app watchers after Stop: %d -> %d", n, got)
	}
}
