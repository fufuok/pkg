package crontab

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fufuok/cache/xsync"
	"github.com/fufuok/cron"
	"github.com/fufuok/utils/xid"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/alarm"
)

var (
	ErrJobBlocked = errors.New("job is blocked")
	BlockedLimit  = 2 * time.Second

	// 工作中的任务列表
	jobs *xsync.Map[string, *Job]
)

type Runner interface {
	Run(ctx context.Context) error
}

type Job struct {
	name string
	spec string

	id     cron.EntryID
	ctx    context.Context
	cancel context.CancelFunc

	// 任务是否被调度运行中
	running atomic.Bool
	// 单例执行锁
	runningMu sync.Mutex
}

// 添加或更新任务, 返回工作中的任务对象
func (j *Job) start(ctx context.Context, r Runner, opts ...cron.EntryOption) (*Job, error) {
	j.ctx, j.cancel = context.WithCancel(ctx)
	cmd := func() {
		if skipIfStillRunning.Load() {
			// 每任务单例执行, 不允许任务重叠
			if !j.runningMu.TryLock() {
				logger.Warn().Str("job", j.name).Bool("real_blocked", IsRealBlocked() != nil).
					Msg("Job overlapped and were skipped")
				return
			}
			defer j.runningMu.Unlock()
		}

		start := time.Now()
		rid := xid.NewString()
		logger.Info().Str("job", j.name).Str("rid", rid).Msg("Job start")

		err := r.Run(j.ctx)
		if err != nil {
			alarm.Error().Err(err).Str("job", j.name).Str("rid", rid).Dur("took", time.Since(start)).Msg("Job run")
		}

		logger.Info().Str("job", j.name).Str("rid", rid).Dur("took", time.Since(start)).Msg("Job done")
	}

	id, err := crontab.AddFunc(j.spec, cmd, opts...)
	if err != nil {
		return j, err
	}

	j.id = id
	j.running.Store(true)
	jobs.Store(j.name, j)

	logger.Warn().Str("job", j.name).Str("cron", j.spec).Time("next", j.Next()).Msg("Job added")
	return j, nil
}

func (j *Job) Name() string {
	return j.name
}

func (j *Job) Next() time.Time {
	if !j.IsRunning() {
		return time.Time{}
	}
	return crontab.Entry(j.id).Next
}

func (j *Job) Prev() time.Time {
	if !j.IsRunning() {
		return time.Time{}
	}
	return crontab.Entry(j.id).Prev
}

func (j *Job) IsRunning() bool {
	return j.running.Load()
}

func (j *Job) Stop() {
	if !j.IsRunning() {
		return
	}
	logger.Warn().Str("job", j.name).Str("cron", j.spec).Time("prev", j.Prev()).Msg("Job stopped")
	jobs.Delete(j.name)
	j.running.Store(false)
	crontab.Remove(j.id)
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
}

// AddJob 添加任务
func AddJob(ctx context.Context, name, spec string, runner Runner, opts ...cron.EntryOption) (*Job, error) {
	if job, ok := GetJob(name); ok {
		if job.IsRunning() && job.spec == spec {
			logger.Info().Str(name, spec).Msg("ignore adding job")
			return job, nil
		}
		job.Stop()
	}
	j := &Job{
		name: name,
		spec: spec,
	}
	return j.start(ctx, runner, opts...)
}

// GetJob 通过名称获取任务对象
func GetJob(name string) (*Job, bool) {
	return jobs.Load(name)
}

// StopJob 通过名称停止任务
func StopJob(name string) bool {
	logger.Info().Str("job", name).Msg("about to stop")
	if j, ok := GetJob(name); ok {
		j.Stop()
		return true
	}
	return false
}

// IsRealBlocked 场景:
// 任务设置了立即执行, 00:59.999 刚开始执行,
// 下次执行时间 01:00 跟着就到了, 再次启动了任务, 但没抢到锁, 忽略该次 Blocked
func IsRealBlocked() error {
	if time.Since(common.StartTime) > BlockedLimit {
		return ErrJobBlocked
	}
	return nil
}
