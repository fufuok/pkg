package master

import (
	"sync"
	"sync/atomic"
	"time"
)

var (
	// currentGate 是最近一次启动 scheduler 时建立的重载闸门, Stop 经它与 watcher 协调; 未启动时为 nil.
	currentGate atomic.Pointer[reloadGate]

	// stopWaitTimeout 是 Stop 等待在途重载周期的上限. 超时后记录告警并继续停机, 避免卡死的 Runtime
	// 使进程无法响应退出信号. 常见部署的 systemd TimeoutStopSec 为 5 秒, 取 2 秒为业务 Stop (如队列落盘)
	// 留出余量; 正常 Runtime 为毫秒级, 只有慢 Runtime 恰逢停机时才会用满. 包内测试可缩短, 不支持并发改写.
	stopWaitTimeout = 2 * time.Second
)

// reloadGate 协调 watcher 的检查/重载周期与 Stop, 保证业务资源的关闭不与同步 Runtime 交错:
//   - 请求停机后不再开始新周期, 在途周期在各 Runtime 之间的安全点提前结束 (见 stopRequested);
//   - 每次 Stop 都等待在途周期结束 (上限 stopWaitTimeout) 后才关闭业务, 并发的第二次 Stop 同样等待;
//   - 业务清理 (逆序 Pipeline.Stop) 只由第一个 Stop 执行一次, 其余 Stop 等它完成后返回;
//   - 停机且无在途周期 (或等待超时) 时关闭 quit, scheduler 退出并取消远端拉取任务, watcher 退出并停止 ticker.
//
// Runtime 内同步调用 Stop (重入) 时不等待本周期, 否则会自等待死锁; Pipeline.Stop 内再调 Stop 时不等待清理完成.
// 二者均按 goroutine ID 识别, Runtime 另起 goroutine 调 Stop 并同步等待它仍会受等待上限约束.
// 应用自助 Watcher.Func 由 SafeGo 异步执行, 不属于周期, 不在等待范围内.
type reloadGate struct {
	mu      sync.Mutex
	stopped bool
	// done 在途周期结束时关闭, 无在途周期时为 nil.
	done chan struct{}
	// owner 是在途周期所在 goroutine 的 ID, 用于识别 Runtime 内的重入 Stop; 0 表示未知.
	owner uint64
	// quit 在停机且无在途周期 (或等待超时) 后关闭, 经 quitOnce 只关闭一次.
	quit     chan struct{}
	quitOnce sync.Once
	// cleaned 非 nil 表示已有 Stop 认领业务清理, 清理完成后关闭; cleaner 为认领者 goroutine ID.
	cleaned chan struct{}
	cleaner uint64
}

// newReloadGate 创建未停机的闸门.
func newReloadGate() *reloadGate {
	return &reloadGate{quit: make(chan struct{})}
}

// closeQuit 通知 scheduler 与 watcher 退出, 可重复调用.
func (g *reloadGate) closeQuit() {
	g.quitOnce.Do(func() {
		close(g.quit)
	})
}

// begin 在 watcher 每个周期开始前调用, owner 为 watcher 所在 goroutine ID.
// 已停机返回 false, watcher 应直接退出; 返回 true 时调用方必须在周期结束后调用 end.
func (g *reloadGate) begin(owner uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopped {
		return false
	}
	g.done = make(chan struct{})
	g.owner = owner
	return true
}

// end 标记当前周期结束并唤醒等待中的 Stop. 周期内已请求停机时由这里关闭 quit.
func (g *reloadGate) end() {
	g.mu.Lock()
	close(g.done)
	g.done, g.owner = nil, 0
	stopped := g.stopped
	g.mu.Unlock()
	if stopped {
		g.closeQuit()
	}
}

// stopping 报告是否已请求停机.
func (g *reloadGate) stopping() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopped
}

// requestStop 标记停机并最多等待 timeout 让在途周期结束, self 为调用方 goroutine ID (0 表示未知).
// 重复调用同样等待同一在途周期. 无在途周期时立即关闭 quit; 调用方就是在途周期本身 (Runtime 重入 Stop)
// 时不等待, 交由 end 关闭 quit. 等待超时关闭 quit 并返回 false, 卡住的周期恢复后在安全点自行结束.
func (g *reloadGate) requestStop(self uint64, timeout time.Duration) bool {
	g.mu.Lock()
	g.stopped = true
	done, owner := g.done, g.owner
	g.mu.Unlock()

	if done == nil {
		g.closeQuit()
		return true
	}
	if owner != 0 && owner == self {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		g.closeQuit()
		return false
	}
}

// claimCleanup 认领业务清理. 第一个调用者返回 first=true, 必须在清理后调用 finishCleanup;
// 其余调用者返回需等待的完成信号. 以下两种调用者返回 nil, 不等待:
//   - 清理者自身 (Pipeline.Stop 内重入 Stop), 否则等待自己;
//   - 在途周期的 owner (Runtime 内重入 Stop, 而清理已由等待超时的外部 Stop 认领): 外部清理中的
//     Pipeline.Stop 可能正等待该 Runtime 退出, owner 再等清理完成会形成循环等待.
func (g *reloadGate) claimCleanup(self uint64) (first bool, wait <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cleaned == nil {
		g.cleaned = make(chan struct{})
		g.cleaner = self
		return true, nil
	}
	if self != 0 && (self == g.cleaner || self == g.owner) {
		return false, nil
	}
	return false, g.cleaned
}

// finishCleanup 标记业务清理完成, 唤醒等待中的其余 Stop.
func (g *reloadGate) finishCleanup() {
	g.mu.Lock()
	defer g.mu.Unlock()
	close(g.cleaned)
}

// notifyScheduler 向 scheduler 发送重载/重启信号 (ch 为 reloadChan 或 restartChan).
// Stop 等待超时后 scheduler 会先于在途周期退出, 无缓冲发送将永久阻塞 watcher; 因此发送随 quit 放弃,
// 停机期间丢弃重载/重启信号不影响结果. 未启动 scheduler (currentGate 为 nil, 如包内单测直接调用) 时按原方式发送.
func notifyScheduler(ch chan bool) {
	g := currentGate.Load()
	if g == nil {
		ch <- true
		return
	}
	select {
	case ch <- true:
	case <-g.quit:
	}
}

// stopRequested 报告当前闸门是否已请求停机, 供重载周期在各 Runtime 之间提前结束:
// 停机后 (含 Runtime 内重入 Stop 已关闭业务) 不再对其余模块执行 Runtime. 未启动 scheduler 时为 false.
func stopRequested() bool {
	g := currentGate.Load()
	return g != nil && g.stopping()
}
