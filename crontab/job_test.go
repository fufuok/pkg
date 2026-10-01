package crontab

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fufuok/cron"
	"github.com/fufuok/pkg/assert"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

func TestMain(m *testing.M) {
	// 执行测试环境初始化
	config.InitTester()
	common.InitTester()
	InitTester()

	exitCode := m.Run()

	// 测试环境执行清理
	StopTester()
	common.StopTester()
	config.StopTester()

	os.Exit(exitCode)
}

// testWithSchedulerBubble 为串行调度测试建立独立的虚拟时间环境.
// TestMain 的调度器必须在 bubble 外停止并等待, 不能让外部 goroutine 访问内部 channel.
// 内部任务全部结束后才恢复原调度器, 任务表和单例开关; 使用本助手的测试不能并行运行.
func testWithSchedulerBubble(t *testing.T, test func(*testing.T)) {
	t.Helper()
	previousCron, previousJobs := crontab, jobs
	previousSkip := skipIfStillRunning.Load()
	<-previousCron.Stop().Done()
	defer func() {
		crontab, jobs = previousCron, previousJobs
		skipIfStillRunning.Store(previousSkip)
		previousCron.Start()
	}()

	synctest.Test(t, func(t *testing.T) {
		// 在 bubble 内创建 cron 的 channel, timer 和 goroutine, 沿用生产初始化选项.
		initMain()
		skipIfStillRunning.Store(false)
		t.Cleanup(func() {
			jobs.Range(func(_ string, job *Job) bool {
				job.Stop()
				return true
			})
			// 生产 Stop 只发出停止请求, 测试还需等待全部任务返回才能离开 bubble.
			<-crontab.Stop().Done()
		})
		test(t)
	})
}

// MockRunner 是一个模拟的 Runner 实现
type MockRunner struct {
	runCount atomic.Int32
	runError error
	runFunc  func()
}

func (m *MockRunner) Run(ctx context.Context) error {
	m.runCount.Add(1)
	if m.runFunc != nil {
		m.runFunc()
	}
	return m.runError
}

// RunCount 并发安全地返回任务执行次数, 供调度器异步测试读取.
func (m *MockRunner) RunCount() int {
	return int(m.runCount.Load())
}

func TestAddJob(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{
			name:    "valid_job",
			spec:    "@every 1s",
			wantErr: false,
		},
		{
			name:    "invalid_cron_spec",
			spec:    "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRunner := &MockRunner{}
			ctx := context.Background()

			job, err := AddJob(ctx, tt.name, tt.spec, mockRunner)

			if tt.wantErr {
				assert.NotNil(t, err)
				assert.Nil(t, job)
				_, exists := GetJob(tt.name)
				assert.False(t, exists)
				return
			}

			assert.Nil(t, err)
			assert.NotNil(t, job)
			assert.Equal(t, tt.name, job.Name())
			assert.True(t, job.IsRunning())
			t.Cleanup(job.Stop)
		})
	}
}

// TestAddOnceJob 验证单次任务执行后移除, 后续调度不再执行, 同名注册仍复用原任务.
func TestAddOnceJob(t *testing.T) {
	t.Run("once_job_execution", func(t *testing.T) {
		testWithSchedulerBubble(t, func(t *testing.T) {
			mockRunner := &MockRunner{}
			job, err := AddOnceJob(t.Context(), "once_test", "@every 1s", mockRunner)
			if err != nil {
				t.Fatal(err)
			}

			// 推进至首次调度并等待 goroutine 收敛, 不依赖真实时间或状态轮询.
			synctest.Sleep(time.Second)
			assert.Equal(t, 1, mockRunner.RunCount())
			assert.False(t, job.IsRunning())
			_, exists := GetJob(job.Name())
			assert.False(t, exists)
			assert.Equal(t, 0, len(crontab.Entries()))

			// 再经过两个调度周期, 确认单次任务已经从调度器移除.
			synctest.Sleep(2 * time.Second)
			assert.Equal(t, 1, mockRunner.RunCount())
		})
	})

	t.Run("duplicate_once_job", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx := context.Background()

		// 添加第一个一次性任务
		job1, err := AddOnceJob(ctx, "duplicate_once", "@every 100ms", mockRunner)
		assert.Nil(t, err)

		// 尝试添加同名任务应该返回相同的任务实例
		job2, err := AddOnceJob(ctx, "duplicate_once", "@every 100ms", mockRunner)
		assert.Nil(t, err)

		// 应该是同一个任务实例
		assert.Equal(t, job1, job2)

		// 清理
		if job1.IsRunning() {
			job1.Stop()
		}
	})
}

func TestAddJobDuplicate(t *testing.T) {
	t.Run("add_duplicate_job_same_spec", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx := context.Background()

		// 添加第一个任务
		job1, err := AddJob(ctx, "duplicate_test", "@every 1s", mockRunner)
		assert.Nil(t, err)

		// 添加相同名称和规格的任务应返回相同实例
		job2, err := AddJob(ctx, "duplicate_test", "@every 1s", mockRunner)
		assert.Nil(t, err)
		assert.Equal(t, job1, job2)

		// 清理
		job1.Stop()
	})

	t.Run("add_duplicate_job_different_spec", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx := context.Background()

		// 添加第一个任务
		job1, err := AddJob(ctx, "duplicate_diff_spec", "@every 1s", mockRunner)
		assert.Nil(t, err)

		// 添加相同名称但不同规格的任务应停止旧任务并创建新任务
		job2, err := AddJob(ctx, "duplicate_diff_spec", "@every 2s", mockRunner)
		assert.Nil(t, err)
		assert.NotEqual(t, job1, job2)
		assert.False(t, job1.IsRunning())

		// 清理
		job2.Stop()
	})

	t.Run("invalid_spec_preserves_existing_job", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx := context.Background()

		job, err := AddJob(ctx, "keep_on_bad_spec", "@every 1s", mockRunner)
		assert.Nil(t, err)
		assert.NotNil(t, job)
		t.Cleanup(job.Stop)

		for _, spec := range []string{"invalid", ""} {
			got, err := AddJob(ctx, "keep_on_bad_spec", spec, mockRunner)
			assert.NotNil(t, err, spec)
			assert.Nil(t, got, spec)
			assert.True(t, job.IsRunning(), spec)

			cur, ok := GetJob("keep_on_bad_spec")
			assert.True(t, ok, spec)
			assert.Equal(t, job, cur, spec)
			assert.Equal(t, "@every 1s", cur.spec, spec)
		}
	})
}

// TestJobExecutionWithSkipIfStillRunning 验证首次执行未返回时, 下一周期是否允许重叠进入 Runner.
func TestJobExecutionWithSkipIfStillRunning(t *testing.T) {
	for _, tt := range []struct {
		name      string
		skip      bool
		wantCount int
	}{
		{name: "skip_if_still_running_blocks_overlap", skip: true, wantCount: 1},
		{name: "not_skip_if_still_running_allows_overlap", skip: false, wantCount: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testWithSchedulerBubble(t, func(t *testing.T) {
				SetSkipIfStillRunning(tt.skip)
				started := make(chan struct{}, 2)
				release := make(chan struct{})
				mockRunner := &MockRunner{runFunc: func() {
					started <- struct{}{}
					<-release
				}}
				job, err := AddJob(t.Context(), "overlap_test", "@every 1s", mockRunner, cron.WithRunImmediately())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					// 先移除调度, 再释放已进入的执行; 助手随后等待它们全部结束.
					job.Stop()
					close(release)
				})

				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for first job execution")
				}
				// 首次执行仍被 release 阻塞, 精确推进一个周期后检查重叠进入次数.
				// Sleep 同时等待该时刻的任务收敛, 避免与 cron 的同刻 timer 抢先断言.
				synctest.Sleep(time.Second)
				assert.Equal(t, tt.wantCount, mockRunner.RunCount())
			})
		})
	}
}

func TestStopJob(t *testing.T) {
	t.Run("stop_existing_job", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx := context.Background()

		// 添加任务
		_, err := AddJob(ctx, "stop_test", "@every 1s", mockRunner)
		assert.Nil(t, err)

		// 停止任务
		stopped := StopJob("stop_test")
		assert.True(t, stopped)

		// 验证任务已停止
		job, exists := GetJob("stop_test")
		assert.False(t, exists)
		assert.Nil(t, job)
	})

	t.Run("stop_nonexistent_job", func(t *testing.T) {
		// 停止不存在的任务应返回 false
		stopped := StopJob("nonexistent_job")
		assert.False(t, stopped)
	})
}

// TestJobContextCancellation 验证父 context 取消只通知 Runner, 不自动撤销周期任务.
func TestJobContextCancellation(t *testing.T) {
	testWithSchedulerBubble(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var calls atomic.Int32
		runner := publicationRunner(func(runCtx context.Context) error {
			calls.Add(1)
			assert.Equal(t, context.Canceled, runCtx.Err())
			return nil
		})
		job, err := AddJob(ctx, "context_cancel_test", "@every 1s", runner)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		synctest.Sleep(2 * time.Second)
		assert.Equal(t, int32(2), calls.Load())
		assert.True(t, job.IsRunning())
		assert.True(t, StopJob(job.Name()))
		synctest.Sleep(time.Second)
		assert.Equal(t, int32(2), calls.Load())
		assert.Empty(t, crontab.Entries())
	})
}
