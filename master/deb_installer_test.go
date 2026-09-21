package master

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// debFixture 仅模拟整数版本的顺序和命令结果; Debian特殊版本规则由Linux真实dpkg测试覆盖.
type debFixture struct {
	callsMu                                                     sync.Mutex
	target                                                      debTarget
	version                                                     string
	calls                                                       []string
	updateFailures, installFailures                             int
	queryFailure, interrupted, configureFailure, writeOnFailure bool
	hook                                                        func(string)
}

// newDebFixture 将时间交给synctest控制, 所有模拟命令仍通过真实worker串行调度.
func newDebFixture(t *testing.T) (*debInstaller, *debFixture) {
	t.Helper()
	f := &debFixture{target: debTarget{version: "2", threshold: 100}, version: "1"}
	u := newDebInstaller("test-pkg", func() debTarget { return f.target })
	u.ready = func() error { return nil }
	u.eligible = func(_ string, threshold uint64) bool { return threshold > 0 && threshold <= 100 }
	u.run = f.run
	t.Cleanup(func() { u.stop(); <-u.done })
	return u, f
}

// run 模拟外部状态变化, 在hook中可阻塞任意阶段以构造真实命令交错.
func (f *debFixture) run(_ time.Duration, args ...string) debCommandResult {
	stage := args[1]
	switch args[0] {
	case debDpkgQuery:
		stage = "query"
	case debAPTGet:
		if args[len(args)-1] == "update" {
			stage = "update"
		} else {
			stage = "install:" + strings.SplitN(args[len(args)-1], "=", 2)[1]
		}
	case debDpkg:
		if slices.Contains(args, "--configure") {
			stage = "configure"
		}
	}
	f.callsMu.Lock()
	f.calls = append(f.calls, stage)
	f.callsMu.Unlock()
	if f.hook != nil {
		f.hook(stage)
	}
	switch {
	case stage == "query":
		if f.queryFailure {
			return debFailed("database unavailable", 2)
		}
		return debCommandResult{output: "installed\t" + f.version + "\n"}
	case stage == "--validate-version":
		if strings.Contains(args[2], ";") {
			return debFailed("invalid version", 2)
		}
	case stage == "--compare-versions":
		if args[3] == "gt" && args[2] <= args[4] {
			return debFailed("", 1)
		}
		if args[3] == "eq" && args[2] != args[4] {
			return debFailed("", 1)
		}
	case stage == "update":
		if f.updateFailures > 0 {
			f.updateFailures--
			return debFailed("repository unavailable", 100)
		}
	case stage == "configure":
		if f.configureFailure {
			return debFailed("postinst failed", 2)
		}
	case strings.HasPrefix(stage, "install:"):
		if f.installFailures > 0 {
			f.installFailures--
			if f.writeOnFailure {
				f.version = strings.TrimPrefix(stage, "install:")
			}
			if f.interrupted {
				return debFailed("dpkg was interrupted, run 'dpkg --configure -a'", 100)
			}
			return debFailed("package installation failed", 100)
		}
		f.version = strings.TrimPrefix(stage, "install:")
	}
	return debCommandResult{}
}

// recorded 在持锁期间复制命令记录, 虚拟时间推进本身不保证后续写入的内存同步.
func (f *debFixture) recorded() []string {
	f.callsMu.Lock()
	defer f.callsMu.Unlock()
	return slices.Clone(f.calls)
}

// debFailed 表示命令真实退出且失败, 与启动失败的exit负值区别.
func debFailed(output string, exit int) debCommandResult {
	return debCommandResult{output: output, exit: exit, err: errors.New("fixture command failed")}
}

// debActions 只提取会改变系统状态的命令, 查询次数不作为行为契约.
func debActions(calls []string) []string {
	var actions []string
	for _, call := range calls {
		if call == "update" || call == "configure" || strings.HasPrefix(call, "install:") {
			actions = append(actions, call)
		}
	}
	return actions
}

// requireDebActions 验证真实调度产生的命令序列, 不要求内部查询实现固定.
func requireDebActions(t *testing.T, f *debFixture, want ...string) {
	t.Helper()
	synctest.Wait()
	if got := debActions(f.recorded()); !slices.Equal(got, want) {
		t.Fatalf("actions %v, want %v", got, want)
	}
}

// TestDebInstallerRetryBudget 验证初始化不安装, 一分钟只补试一次, 同值事件及重复wake不重置在途预算.
func TestDebInstallerRetryBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u, f := newDebFixture(t)
		time.Sleep(time.Hour)
		requireDebActions(t, f)
		f.installFailures = 3
		u.publish()
		synctest.Wait()
		requireDebActions(t, f, "update", "install:2")
		time.Sleep(30 * time.Second)
		u.publish()
		synctest.Wait()
		time.Sleep(29 * time.Second)
		requireDebActions(t, f, "update", "install:2")
		time.Sleep(time.Second)
		synctest.Wait()
		requireDebActions(t, f, "update", "install:2", "update", "install:2")
		u.wake <- struct{}{}
		time.Sleep(time.Hour)
		requireDebActions(t, f, "update", "install:2", "update", "install:2")
		// 真正的新配置事件在轮次已结束后可重新尝试相同版本.
		u.publish()
		synctest.Wait()
		requireDebActions(t, f, "update", "install:2", "update", "install:2", "update", "install:2")
	})
}

// TestDebInstallerUpdateFailureContinues 验证两层补试的总预算, 索引失败不吞掉install.
func TestDebInstallerUpdateFailureContinues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u, f := newDebFixture(t)
		f.updateFailures, f.installFailures = 10, 10
		u.publish()
		time.Sleep(80 * time.Second)
		synctest.Wait()
		requireDebActions(t, f, "update", "update", "install:2", "update", "update", "install:2")
		time.Sleep(time.Hour)
		requireDebActions(t, f, "update", "update", "install:2", "update", "update", "install:2")
	})
}

// TestDebInstallerUpdateFailureThenInstallSuccess 验证update错误不会制造多余的安装补试.
func TestDebInstallerUpdateFailureThenInstallSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u, f := newDebFixture(t)
		f.updateFailures = 10
		u.publish()
		time.Sleep(time.Hour)
		synctest.Wait()
		requireDebActions(t, f, "update", "update", "install:2")
	})
}

// TestDebInstallerConditionalConfigure 验证同版失败仍有唯一补试, 仅明确中断触发一次configure.
func TestDebInstallerConditionalConfigure(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary_failure", true: "dpkg_interrupted"}[interrupted], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				f.installFailures, f.writeOnFailure = 1, true
				f.interrupted, f.configureFailure = interrupted, true
				u.publish()
				time.Sleep(time.Hour)
				synctest.Wait()
				if interrupted {
					requireDebActions(t, f, "update", "install:2", "update", "configure", "install:2")
				} else {
					requireDebActions(t, f, "update", "install:2", "update", "install:2")
				}
			})
		})
	}
}

// TestDebInstallerReplacesWaitingTarget 验证十秒和一分钟等待均可由新目标取消, 新目标有自己的预算.
func TestDebInstallerReplacesWaitingTarget(t *testing.T) {
	for _, indexWait := range []bool{false, true} {
		t.Run(map[bool]string{false: "install_wait", true: "update_wait"}[indexWait], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				if indexWait {
					f.updateFailures = 1
				} else {
					f.installFailures = 1
				}
				u.publish()
				synctest.Wait()
				f.target.version = "3"
				u.publish()
				synctest.Wait()
				time.Sleep(time.Hour)
				if indexWait {
					requireDebActions(t, f, "update", "update", "install:3")
				} else {
					requireDebActions(t, f, "update", "install:2", "update", "install:3")
				}
			})
		})
	}
}

// TestDebInstallerCommandReplacement 命令进行中发布多次配置, 当前命令结束后只能执行最后目标.
func TestDebInstallerCommandReplacement(t *testing.T) {
	for _, stage := range []string{"query", "update", "configure", "install:2"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				entered, release := make(chan struct{}), make(chan struct{})
				fired := false
				f.hook = func(command string) {
					if command == stage && !fired {
						fired = true
						close(entered)
						<-release
					}
				}
				if stage == "configure" {
					f.installFailures = 1
					f.interrupted = true
				}
				u.publish()
				<-entered
				before := len(f.recorded())
				for range 10 {
					f.target.version = "4"
					u.publish()
					f.target.version = "3"
					u.publish()
				}
				synctest.Wait()
				if len(f.recorded()) != before {
					t.Fatal("another command started while previous command was running")
				}
				close(release)
				synctest.Wait()
				time.Sleep(time.Hour)
				got := debActions(f.recorded())
				if got[len(got)-1] != "install:3" || slices.Contains(got, "install:4") {
					t.Fatalf("stale target: %v", got)
				}
				count := 0
				for _, call := range got {
					if call == "install:2" {
						count++
					}
				}
				want := 0
				if stage == "install:2" || stage == "configure" {
					want = 1
				}
				if count != want {
					t.Fatalf("old install count %d, want %d; %v", count, want, got)
				}
			})
		})
	}
}

// TestDebInstallerRevocationAndPause 覆盖0比例、非法比例、空目标和加载失败撤销, 不复活旧等待.
func TestDebInstallerRevocationAndPause(t *testing.T) {
	for _, reason := range []string{"zero", "invalid", "empty", "load_failure"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				f.installFailures = 2
				u.publish()
				synctest.Wait()
				switch reason {
				case "zero":
					f.target.threshold = 0
				case "invalid":
					f.target.threshold = 101
				case "empty":
					f.target.version = ""
				case "load_failure":
					u.pause()
				}
				if reason != "load_failure" {
					u.publish()
				}
				time.Sleep(time.Hour)
				synctest.Wait()
				requireDebActions(t, f, "update", "install:2")
				if reason == "load_failure" {
					u.publish()
					synctest.Wait()
					requireDebActions(t, f, "update", "install:2", "update", "install:2")
				}
			})
		})
	}
}

// TestDebInstallerStopDoesNotKillCommand 验证Stop立即返回, 已运行命令可以结束但不能再补试.
func TestDebInstallerStopDoesNotKillCommand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u, f := newDebFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		f.hook = func(stage string) {
			if stage == "install:2" {
				close(entered)
				<-release
			}
		}
		f.installFailures = 2
		u.publish()
		<-entered
		u.stop()
		u.publish()
		synctest.Wait()
		select {
		case <-u.done:
			t.Fatal("worker returned before command finished")
		default:
		}
		close(release)
		<-u.done
		time.Sleep(time.Hour)
		requireDebActions(t, f, "update", "install:2")
	})
}

// TestDebInstallerVersionGates 验证相等、降级、非法输入和查询错误都不会被当成可升级版本.
func TestDebInstallerVersionGates(t *testing.T) {
	for _, kind := range []string{"equal", "downgrade", "invalid", "query_failure", "upgrade_during_update", "upgrade_during_retry"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				switch kind {
				case "equal":
					f.version = "2"
				case "downgrade":
					f.version = "3"
				case "invalid":
					f.target.version = "2;rm"
				case "query_failure":
					f.queryFailure = true
				case "upgrade_during_update":
					f.hook = func(stage string) {
						if stage == "update" {
							f.version = "3"
						}
					}
				case "upgrade_during_retry":
					f.installFailures = 1
					f.hook = func(stage string) {
						if stage == "install:2" {
							f.version = "3"
						}
					}
				}
				u.publish()
				time.Sleep(time.Hour)
				synctest.Wait()
				if kind == "upgrade_during_update" {
					requireDebActions(t, f, "update")
				} else if kind == "upgrade_during_retry" {
					requireDebActions(t, f, "update", "install:2")
				} else {
					requireDebActions(t, f)
				}
				if kind == "query_failure" {
					count := 0
					for _, call := range f.recorded() {
						if call == "query" {
							count++
						}
					}
					if count != 2 {
						t.Fatalf("query attempts=%d, want 2", count)
					}
				}
			})
		})
	}
}

// TestDebInstallerReadsLoadedConfig 验证未通知的既有加载入口也无法让补试执行旧版本.
func TestDebInstallerReadsLoadedConfig(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		u, f := newDebFixture(t)
		f.installFailures = 1
		// 模拟config.Config的原子快照, 时间推进本身不构成跨协程内存同步.
		var loaded atomic.Pointer[debTarget]
		loaded.Store(&debTarget{version: "2", threshold: 100})
		u.read = func() debTarget { return *loaded.Load() }
		u.publish()
		synctest.Wait()
		loaded.Store(&debTarget{version: "3", threshold: 100})
		time.Sleep(time.Minute)
		synctest.Wait()
		requireDebActions(t, f, "update", "install:2", "update", "install:3")
	})
}
