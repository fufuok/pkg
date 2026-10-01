package crontab

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fufuok/cron"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
)

// stopLogBarrier 放大旧任务停止与同名重建之间的原有窗口, 不向生产实现增加测试钩子.
type stopLogBarrier struct {
	entered chan struct{}
	release chan struct{}
	blocked atomic.Bool
}

// Write 仅暂停第一次停止日志, 其余日志直接消费, 使交错顺序可确定地重现.
func (w *stopLogBarrier) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"message":"Job stopped"`)) && w.blocked.CompareAndSwap(false, true) {
		close(w.entered)
		<-w.release
	}
	return len(p), nil
}

// useJobTestLogger 在串行测试中替换日志出口, 调用方须在测试结束前释放并等待异步写入.
func useJobTestLogger(t *testing.T, w io.Writer) {
	t.Helper()
	previousLogger, previousLevel := *common.Log(), zerolog.GlobalLevel()
	*common.Log() = zerolog.New(w)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		*common.Log() = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
}

// TestJobStopPreservesReplacement 验证旧 once 收尾只删除自身登记, 不误删热重载后的同名任务.
func TestJobStopPreservesReplacement(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		w := &stopLogBarrier{entered: make(chan struct{}), release: make(chan struct{})}
		useJobTestLogger(t, w)
		release := sync.OnceFunc(func() { close(w.release) })
		defer release()
		old, err := AddOnceJob(t.Context(), "replacement", "@every 24h", &MockRunner{}, cron.WithRunImmediately())
		if err != nil {
			t.Fatal(err)
		}
		<-w.entered
		// 旧 Stop 已置 running=false, 热重载再次 Stop 会立即返回, 然后登记新任务.
		old.Stop()
		fresh, err := AddJob(t.Context(), old.Name(), "@every 23h", &MockRunner{})
		if err != nil {
			t.Fatal(err)
		}
		release()
		synctest.Wait()
		got, ok := GetJob(fresh.Name())
		assert.True(t, ok)
		assert.Equal(t, fresh, got)
		assert.True(t, fresh.IsRunning())
		assert.Nil(t, fresh.ctx.Err())
		assert.Equal(t, context.Canceled, old.ctx.Err())
		assert.Equal(t, fresh.id, crontab.Entry(fresh.id).ID)
		assert.False(t, crontab.Entry(old.id).Valid())
		assert.True(t, StopJob(fresh.Name()), "replacement must remain stoppable by name")
		assert.Empty(t, crontab.Entries())
	})
}

// TestOnceJobCleanup 验证普通返回、错误和 panic 都只尝试一次并释放登记、条目与 context.
func TestOnceJobCleanup(t *testing.T) {
	for _, outcome := range []string{"success", "error", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			testWithSchedulerBubble(t, func(t *testing.T) {
				runner := &MockRunner{runFunc: func() {
					if outcome == "panic" {
						panic("test once failure")
					}
				}}
				if outcome == "error" {
					runner.runError = errors.New("test once failure")
				}
				job, err := AddOnceJob(t.Context(), outcome, "@every 1s", runner, cron.WithRunImmediately())
				if err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				assert.Equal(t, 1, runner.RunCount())
				assert.False(t, job.IsRunning())
				assert.Equal(t, context.Canceled, job.ctx.Err())
				_, ok := GetJob(job.Name())
				assert.False(t, ok)
				assert.Empty(t, crontab.Entries())
				synctest.Sleep(2 * time.Second)
				assert.Equal(t, 1, runner.RunCount())
				// 同名同 spec 在一次执行结束后可以重新注册, 不会复用已耗尽的 executed 状态.
				fresh, err := AddOnceJob(t.Context(), job.Name(), job.spec, &MockRunner{})
				assert.Nil(t, err)
				assert.NotEqual(t, job, fresh)
			})
		})
	}
}

// TestOnceOverlapDoesNotStopOwner 验证重复的 once 回调不会清理仍在执行的首次回调.
func TestOnceOverlapDoesNotStopOwner(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		runner := &MockRunner{runFunc: func() { <-release }}
		job, err := AddOnceJob(t.Context(), "once_owner", "@every 1s", runner, cron.WithRunImmediately())
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		synctest.Sleep(2 * time.Second)
		assert.Equal(t, 1, runner.RunCount())
		assert.True(t, job.IsRunning())
		assert.Nil(t, job.ctx.Err())
		got, ok := GetJob(job.Name())
		assert.True(t, ok)
		assert.Equal(t, job, got)
	})
}

// TestStoppedQueuedJobDoesNotRun 用真实调度回调模拟已排队但尚未进入 pkg 执行流程的任务.
func TestStoppedQueuedJobDoesNotRun(t *testing.T) {
	for _, once := range []bool{false, true} {
		name := "periodic"
		if once {
			name = "once"
		}
		t.Run(name, func(t *testing.T) {
			testWithSchedulerBubble(t, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				resume := sync.OnceFunc(func() { close(release) })
				defer resume()
				runner := &MockRunner{}
				gate := func(entry *cron.Entry) {
					wrapped := entry.WrappedJob
					entry.WrappedJob = cron.FuncJob(func() {
						close(entered)
						<-release
						wrapped.Run()
					})
				}
				job, err := addJob(t.Context(), name, "@every 24h", runner, once, nil, cron.WithRunImmediately(), gate)
				if err != nil {
					t.Fatal(err)
				}
				<-entered
				job.Stop()
				resume()
				synctest.Wait()
				assert.Equal(t, 0, runner.RunCount(), "stopped callback must not enter Runner")
				assert.False(t, job.executed.Load())
				assert.Equal(t, context.Canceled, job.ctx.Err())
				assert.Empty(t, crontab.Entries())
			})
		})
	}
}

// TestPeriodicPanicKeepsSchedule 验证周期任务仍由原 Recover 处理 panic, 后续调度继续执行.
func TestPeriodicPanicKeepsSchedule(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		runner := &MockRunner{runFunc: func() { panic("test periodic failure") }}
		job, err := AddJob(t.Context(), "periodic_panic", "@every 1s", runner, cron.WithRunImmediately())
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		synctest.Sleep(time.Second)
		assert.Equal(t, 2, runner.RunCount())
		assert.True(t, job.IsRunning())
		assert.Nil(t, job.ctx.Err())
	})
}

// TestJobStopPreviousRun 验证停止日志省去调度时间字段, 公开时间查询仍返回零值.
func TestJobStopPreviousRun(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		var output bytes.Buffer
		useJobTestLogger(t, &output)
		job, err := AddJob(t.Context(), "stop_previous", "@every 24h", &MockRunner{}, cron.WithRunImmediately())
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		previous := job.Prev()
		assert.False(t, previous.IsZero())
		output.Reset()
		job.Stop()
		assert.Contains(t, `"message":"Job stopped"`, output.String())
		assert.False(t, bytes.Contains(output.Bytes(), []byte(`"prev":`)))
		assert.True(t, job.Prev().IsZero())
		assert.True(t, job.Next().IsZero())
	})
}

// TestJobStopIdempotent 验证并发重复 Stop 和登记已不存在时仍正确取消旧任务, 不重新创建登记.
func TestJobStopIdempotent(t *testing.T) {
	for _, registered := range []bool{true, false} {
		testWithSchedulerBubble(t, func(t *testing.T) {
			job, err := AddJob(t.Context(), "idempotent", "@every 24h", &MockRunner{})
			if err != nil {
				t.Fatal(err)
			}
			if !registered {
				jobs.Delete(job.Name())
			}
			var group sync.WaitGroup
			for range 16 {
				group.Go(job.Stop)
			}
			group.Wait()
			assert.False(t, job.IsRunning())
			assert.Equal(t, context.Canceled, job.ctx.Err())
			assert.Equal(t, 0, jobs.Size())
			assert.Empty(t, crontab.Entries())
		})
	}
}

// TestSameSpecPreservesRegistration 验证同 spec 复用不替换 Runner、context、日志字段、once 或选项.
func TestSameSpecPreservesRegistration(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		original, replacement := &MockRunner{}, &MockRunner{}
		fields := map[string]any{"source": "original"}
		job, err := AddJobWithFields(t.Context(), "same_spec", "@every 1s", original, fields)
		if err != nil {
			t.Fatal(err)
		}
		fields["source"] = "caller_changed"
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		optionApplied := false
		got, err := AddOnceJobWithFields(ctx, job.Name(), job.spec, replacement,
			map[string]any{"source": "replacement"}, cron.WithRunImmediately(), func(*cron.Entry) { optionApplied = true })
		assert.Nil(t, err)
		assert.Equal(t, job, got)
		assert.False(t, optionApplied)
		assert.Nil(t, job.ctx.Err())
		assert.Equal(t, "original", job.fields["source"])
		synctest.Sleep(2 * time.Second)
		assert.Equal(t, 2, original.RunCount())
		assert.Equal(t, 0, replacement.RunCount())
		assert.True(t, job.IsRunning())
	})
}

// TestJobStopDoesNotWaitForRunner 验证 Stop 取消 context 但不等待已进入 Runner 的业务完成.
func TestJobStopDoesNotWaitForRunner(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		release, finished := make(chan struct{}), make(chan struct{})
		defer close(release)
		runner := publicationRunner(func(ctx context.Context) error {
			defer close(finished)
			<-ctx.Done()
			<-release
			return nil
		})
		job, err := AddJob(t.Context(), "in_flight", "@every 1s", runner, cron.WithRunImmediately())
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		job.Stop()
		synctest.Sleep(2 * time.Second)
		assert.Equal(t, context.Canceled, job.ctx.Err())
		assert.False(t, job.IsRunning())
		select {
		case <-finished:
			t.Fatal("Stop must not force an in-flight Runner to finish")
		default:
		}
		assert.Empty(t, crontab.Entries())
	})
}
