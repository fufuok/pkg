package crontab

import (
	"context"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fufuok/cron"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/assert"
)

// publicationRunner 让测试直接观察回调收到的 context, 不依赖业务任务或外部资源.
type publicationRunner func(context.Context) error

// Run 保留 Runner 的返回错误和取消语义, 由调度器执行真实的任务收尾.
func (run publicationRunner) Run(ctx context.Context) error {
	return run(ctx)
}

// TestImmediateJobPublication 验证真实立即任务只在登记完成后执行, 短任务可以完整清理自身.
// 多次注册覆盖 AddFunc 返回和异步回调竞争的窗口, 停止调度器并等待全部回调后才检查登记.
func TestImmediateJobPublication(t *testing.T) {
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.Disabled)
	t.Cleanup(func() { zerolog.SetGlobalLevel(previousLevel) })

	for _, tt := range []struct {
		name   string
		once   bool
		cancel bool
	}{
		{name: "once", once: true},
		{name: "runner_stops_job"},
		{name: "canceled_once", once: true, cancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// 真实时钟才能覆盖已到期 timer 与 AddFunc 返回的竞争; 每个场景隔离全局调度器.
			previousCron, previousJobs := crontab, jobs
			previousSkip := skipIfStillRunning.Load()
			<-previousCron.Stop().Done()
			initMain()
			skipIfStillRunning.Store(false)
			t.Cleanup(func() {
				<-crontab.Stop().Done()
				jobs.Range(func(_ string, job *Job) bool {
					job.Stop()
					return true
				})
				crontab, jobs = previousCron, previousJobs
				skipIfStillRunning.Store(previousSkip)
				previousCron.Start()
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}

			const attempts = 512
			calls, unpublished := 0, 0
			added := make([]*Job, 0, attempts)
			timeout := time.NewTimer(10 * time.Second)
			defer timeout.Stop()
			for i := range attempts {
				name := tt.name + "_" + strconv.Itoa(i)
				done := make(chan struct{})
				runner := publicationRunner(func(runCtx context.Context) error {
					defer close(done)
					calls++
					job, ok := GetJob(name)
					if !ok || job.id == 0 || !job.IsRunning() || job.Next().IsZero() {
						unpublished++
					}
					if tt.cancel {
						assert.Equal(t, context.Canceled, runCtx.Err())
					}
					if !tt.once {
						// 普通任务也可在首次回调内按名停止, 不应因尚未登记而漏停.
						StopJob(name)
					}
					return nil
				})
				job, err := addJob(ctx, name, "@every 24h", runner, tt.once, nil, cron.WithRunImmediately())
				if err != nil {
					t.Fatal(err)
				}
				added = append(added, job)
				select {
				case <-done:
				case <-timeout.C:
					t.Fatal("timed out waiting for immediate job execution")
				}
			}

			// Runner 返回后的 once Stop 仍在回调中, 不能仅凭 done 判断清理结束.
			<-crontab.Stop().Done()
			retained := 0
			for _, job := range added {
				if _, ok := GetJob(job.Name()); ok || job.IsRunning() {
					retained++
				}
			}
			t.Logf("calls=%d unpublished=%d retained=%d entries=%d", calls, unpublished, retained, len(crontab.Entries()))
			assert.Equal(t, attempts, calls)
			assert.Equal(t, 0, unpublished, "runner observed incomplete registration")
			assert.Equal(t, 0, retained, "completed jobs remained registered or running")
			assert.Equal(t, 0, jobs.Size())
			assert.Empty(t, crontab.Entries())
		})
	}
}

// TestJobStartFailureCancelsContext 验证 AddFunc 拒绝任务时释放派生 context, 不启动或登记回调.
// 直接调用 start 覆盖预检之后的失败边界, 避免只测到 addJob 的表达式预检.
func TestJobStartFailureCancelsContext(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		job := &Job{name: "invalid_start", spec: "invalid"}
		runner := &MockRunner{}
		got, err := job.start(t.Context(), runner, true, cron.WithRunImmediately())
		if err == nil {
			t.Fatal("expected invalid cron spec to fail")
		}
		assert.Contains(t, `add job "invalid_start"`, err.Error())
		assert.Nil(t, got)
		assert.Equal(t, context.Canceled, job.ctx.Err())
		assert.Nil(t, job.cancel)
		synctest.Wait()
		assert.Equal(t, 0, runner.RunCount())
		assert.False(t, job.IsRunning())
		assert.Equal(t, 0, jobs.Size())
		assert.Empty(t, crontab.Entries())
	})
}
