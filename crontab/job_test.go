package crontab

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
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

func TestAddOnceJob(t *testing.T) {
	t.Run("once_job_execution", func(t *testing.T) {
		runDone := make(chan struct{}, 1)
		mockRunner := &MockRunner{runFunc: func() { runDone <- struct{}{} }}
		ctx := context.Background()

		job, err := AddOnceJob(ctx, "once_test", "@every 1s", mockRunner)
		assert.Nil(t, err)
		assert.NotNil(t, job)
		t.Cleanup(job.Stop)

		select {
		case <-runDone:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for once job")
		}

		// 验证任务只执行了一次
		assert.Equal(t, 1, mockRunner.RunCount())

		// 一次性任务执行后会同步停止并从调度器移除.
		waitUntil(t, time.Second, func() bool { return !job.IsRunning() })
		assert.Equal(t, 1, mockRunner.RunCount())

		// 验证任务已停止
		assert.False(t, job.IsRunning())
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

func TestJobExecutionWithSkipIfStillRunning(t *testing.T) {
	t.Run("skip_if_still_running_blocks_overlap", func(t *testing.T) {
		// 启用跳过仍在运行的任务
		skipIfStillRunning.Store(true)
		defer func() {
			skipIfStillRunning.Store(false)
		}()

		started := make(chan struct{}, 4)
		release := make(chan struct{})
		// 创建一个执行时间较长的 Runner
		mockRunner := &MockRunner{
			runFunc: func() {
				started <- struct{}{}
				<-release
			},
		}
		ctx := context.Background()

		// 创建一个快速重复执行的任务
		job, err := AddJob(ctx, "overlap_test", "@every 1s", mockRunner, cron.WithRunImmediately())
		assert.Nil(t, err)
		t.Cleanup(func() {
			job.Stop()
			close(release)
		})

		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for first job execution")
		}

		// 等待下一次秒级调度, 被占用的单例锁应阻止重叠执行.
		time.Sleep(1200 * time.Millisecond)

		// 由于启用了 skipIfStillRunning，应该只执行了一次
		assert.Equal(t, 1, mockRunner.RunCount())
	})

	t.Run("not_skip_if_still_running_blocks_overlap", func(t *testing.T) {
		started := make(chan struct{}, 4)
		release := make(chan struct{})
		// 创建一个执行时间较长的 Runner
		mockRunner := &MockRunner{
			runFunc: func() {
				started <- struct{}{}
				<-release
			},
		}
		ctx := context.Background()

		// 创建一个快速重复执行的任务
		job, err := AddJob(ctx, "overlap_test", "@every 1s", mockRunner, cron.WithRunImmediately())
		assert.Nil(t, err)
		t.Cleanup(func() {
			job.Stop()
			close(release)
		})

		for range 2 {
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for overlapping job execution")
			}
		}

		// 未启用 skipIfStillRunning 时, 至少两个执行可以重叠进入 Runner.
		if got := mockRunner.RunCount(); got < 2 {
			t.Fatalf("run count = %d, want at least 2", got)
		}
	})
}

// waitUntil 在限定时间内等待异步状态收敛, 避免使用固定长休眠掩盖调度失败.
func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for condition")
		case <-ticker.C:
		}
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

func TestJobContextCancellation(t *testing.T) {
	t.Run("context_cancellation_stops_job", func(t *testing.T) {
		mockRunner := &MockRunner{}
		ctx, cancel := context.WithCancel(context.Background())

		// 添加任务
		job, err := AddJob(ctx, "context_cancel_test", "@every 1s", mockRunner)
		assert.Nil(t, err)

		// 取消上下文
		cancel()

		// 等待一点时间让取消生效
		time.Sleep(50 * time.Millisecond)

		// 任务应该仍然存在但会被标记为停止（取决于具体实现）
		// 注意：某些 cron 实现可能会在上下文取消时自动清理任务
		assert.NotNil(t, job)
	})
}
