package crontab

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fufuok/cache/xsync"
	"github.com/fufuok/cron"
	"github.com/fufuok/pkg/xid"
	"github.com/rs/zerolog"

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

	// 附加的任务字段
	fields map[string]any

	id     cron.EntryID
	ctx    context.Context
	cancel context.CancelFunc

	// 任务是否被调度运行中
	running atomic.Bool

	// 任务是否已被执行过
	executed atomic.Bool

	// 单例执行锁
	runningMu sync.Mutex
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
	if !j.runningToStop() {
		return
	}
	logger.Warn().Str("job", j.name).Str("cron", j.spec).Time("prev", j.Prev()).Msg("Job stopped")
	jobs.Delete(j.name)
	crontab.Remove(j.id)
	if j.cancel != nil {
		j.cancel()
	}
}

// start 把已通过预检的任务挂到调度器, 成功后写入 jobs 并返回自身.
// AddFunc 在入口已 Parse 的前提下仍可能失败 (调度器未就绪或解析器日后分叉).
// ctx 必须在 AddFunc 之前派生: WithRunImmediately 可能立刻回调, 不能延后赋值.
func (j *Job) start(ctx context.Context, r Runner, once bool, opts ...cron.EntryOption) (*Job, error) {
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

		if once && !j.executed.CompareAndSwap(false, true) {
			logger.Info().Str("job", j.name).Msg("once job already executed, skipping")
			return
		}

		start := time.Now()
		rid := xid.NewString()
		logger.Info().Str("job", j.name).Str("rid", rid).Msg("starting job")

		err := r.Run(j.ctx)
		if err != nil {
			logEvent := alarm.Error().Err(err).Str("job", j.name).Str("rid", rid).Dur("took", time.Since(start))
			j.addLogFields(logEvent).Msg("Job execution failed")
		}

		logger.Info().Str("job", j.name).Str("rid", rid).Dur("took", time.Since(start)).Msg("job completed")

		if once {
			j.Stop()
		}
	}

	id, err := crontab.AddFunc(j.spec, cmd, opts...)
	if err != nil {
		// 任务未入表. 父 ctx 若可取消 (Runtime 热加载 ctx), 不 cancel 会把子 ctx
		// 挂在父树上直到父取消. Stop 对未 running 对象是空操作, 不会代为释放.
		j.cancel()
		j.cancel = nil
		return nil, fmt.Errorf("add job %q: %w", j.name, err)
	}

	j.id = id
	j.running.Store(true)
	jobs.Store(j.name, j)

	logger.Warn().Str("job", j.name).Str("cron", j.spec).Time("next", j.Next()).Msg("Job added")
	return j, nil
}

// 处理日志字段
func (j *Job) addLogFields(event *zerolog.Event) *zerolog.Event {
	if j.fields == nil {
		return event
	}
	for k, v := range j.fields {
		event.Any(k, v)
	}
	return event
}

// 从运行中切换到停止
func (j *Job) runningToStop() bool {
	return j.running.CompareAndSwap(true, false)
}

// AddJob 添加或按名更新任务.
// spec 非法 (含空串) 时返回 error, 不停止同名旧任务; 合法后同 spec skip, 不同则先停再挂.
func AddJob(ctx context.Context, name, spec string, runner Runner, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, false, nil, opts...)
}

// AddOnceJob 添加单次任务, 只会执行一次，执行后自动移除
func AddOnceJob(ctx context.Context, name, spec string, runner Runner, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, true, nil, opts...)
}

// AddJobWithFields 添加带自定义日志字段的任务
// fields: 任务执行时附加到日志中的自定义字段, 这些字段会在错误日志中特别有用
func AddJobWithFields(ctx context.Context, name, spec string, runner Runner, fields map[string]any, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, false, fields, opts...)
}

// AddOnceJobWithFields 添加单次任务, 只会执行一次，执行后自动移除
// fields: 任务执行时附加到日志中的自定义字段, 这些字段会在错误日志中特别有用
func AddOnceJobWithFields(ctx context.Context, name, spec string, runner Runner, fields map[string]any, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, true, fields, opts...)
}

// addJob 是 AddJob / AddOnceJob / *WithFields 的统一入口.
//
// 热加载 Runtime 按单线程调用. 非法 spec 绝不能先 Stop 同名旧任务,
// 否则配置笔误会让线上已挂任务消失, 直到下一次合法重载.
//
// 流程:
//  1. 用 DefaultParser.Parse 预检 (与调度器 WithSecondOptional 字段一致).
//     失败只返回 error, 由调用方记录或处理; 旧任务保持调度; 空 spec 也视为非法.
//  2. spec 合法后: 同名且仍 running 且 spec 未变则 skip,
//     不替换 runner / fields / once / opts, 避免热加载抖动和取消在跑任务.
//  3. 否则先 Stop 再挂新任务.
//
// 清空任务请 StopJob, 不要把空 spec 交给本函数.
func addJob(ctx context.Context, name, spec string, runner Runner, once bool, fields map[string]any, opts ...cron.EntryOption) (*Job, error) {
	if _, err := DefaultParser.Parse(spec); err != nil {
		return nil, fmt.Errorf("invalid cron spec %q for job %q: %w", spec, name, err)
	}

	if job, ok := GetJob(name); ok {
		if job.IsRunning() && job.spec == spec {
			logger.Info().Str("job", job.name).Str("cron", spec).Msg("skipping job add")
			return job, nil
		}
		job.Stop()
	}

	j := &Job{
		name:   name,
		spec:   spec,
		fields: maps.Clone(fields),
	}
	return j.start(ctx, runner, once, opts...)
}

// GetJob 通过名称获取任务对象
func GetJob(name string) (*Job, bool) {
	return jobs.Load(name)
}

// StopJob 通过名称停止任务
func StopJob(name string) bool {
	logger.Info().Str("job", name).Msg("stopping job")
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
