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

// Runner 执行业务任务; context 取消由实现自行处理, 调度器不会强制中断已进入的 Run.
type Runner interface {
	Run(ctx context.Context) error
}

// Job 保存一次注册的调度状态, 停止后不重新启动; 重建同名任务会创建新对象.
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

// Name 返回注册时确定的任务名, 停止后仍可查询.
func (j *Job) Name() string {
	return j.name
}

// Next 返回调度器快照中的下次执行时间; 任务停止或条目不存在时返回零值.
func (j *Job) Next() time.Time {
	if !j.IsRunning() {
		return time.Time{}
	}
	return crontab.Entry(j.id).Next
}

// Prev 返回调度器快照中的上次触发时间, 不代表业务执行完成时间.
// 未设置上次调度时间、任务停止或条目不存在时返回零值; 初始值也可能由注册选项指定.
func (j *Job) Prev() time.Time {
	if !j.IsRunning() {
		return time.Time{}
	}
	return crontab.Entry(j.id).Prev
}

// IsRunning 表示任务是否处于可调度状态, 不表示 Runner 此刻正在执行.
func (j *Job) IsRunning() bool {
	return j.running.Load()
}

// Stop 幂等地撤销本任务的登记、调度条目并取消 context, 不等待已经进入执行流程的回调.
// 并发重复调用可在首次调用清理完之前返回; 已进入 Runner 的业务须自行响应取消.
func (j *Job) Stop() {
	if !j.running.CompareAndSwap(true, false) {
		return
	}
	logger.Warn().Str("job", j.name).Str("cron", j.spec).Msg("Job stopped")
	// 同名新任务可能已经注册, 比较对象身份和删除必须在同一个原子操作中完成.
	// 回调内只作比较, 不调用日志、调度器或其他 map 操作, 避免延长持锁或重入.
	jobs.Compute(j.name, func(actual *Job, loaded bool) (*Job, xsync.ComputeOp) {
		if loaded && actual == j {
			return nil, xsync.DeleteOp
		}
		return actual, xsync.CancelOp
	})
	crontab.Remove(j.id)
	if j.cancel != nil {
		j.cancel()
	}
}

// start 把已通过预检的任务挂到调度器, 成功后写入 jobs 并返回自身.
// AddFunc 在入口已 Parse 的前提下仍可能因解析器配置不同而失败.
// cron 异步启动回调; WithRunImmediately 也必须等 id, running 和 jobs 完整发布后才能执行.
func (j *Job) start(ctx context.Context, r Runner, once bool, opts ...cron.EntryOption) (*Job, error) {
	j.ctx, j.cancel = context.WithCancel(ctx)
	published := make(chan struct{})
	cmd := func() {
		// 防止短任务在 AddFunc 返回前结束, 导致 Stop 漏掉尚未发布的任务登记.
		<-published
		// Remove 不能撤回已排队回调; 跳过检查时已停止的任务, 不强制中断已通过检查的执行.
		// 父 context 取消不等同于 Stop, 仍允许 Runner 接收并自行处理已取消的 context.
		if !j.IsRunning() {
			return
		}
		if skipIfStillRunning.Load() {
			// 每任务单例执行, 不允许任务重叠
			if !j.runningMu.TryLock() {
				logger.Warn().Str("job", j.name).Bool("real_blocked", IsRealBlocked() != nil).
					Msg("Job overlapped and were skipped")
				return
			}
			defer j.runningMu.Unlock()
		}

		if once {
			if !j.executed.CompareAndSwap(false, true) {
				logger.Info().Str("job", j.name).Msg("once job already executed, skipping")
				return
			}
			// 只有取得首次执行资格的回调负责收尾, panic 也清理; 仍由外层 cron Recover 记录异常.
			defer j.Stop()
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
	}

	id, err := crontab.AddFunc(j.spec, cmd, opts...)
	if err != nil {
		// 解析失败时 cron 尚未排队回调, 不存在等待 published 的执行, 无需放行.
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
	// 先记录登记日志再放行回调, 保证开始和停止日志不会早于 Job added.
	close(published)
	return j, nil
}

// addLogFields 附加注册时复制的错误日志字段; 仅浅拷贝 map, 调用方仍须自行保护可变的字段值.
func (j *Job) addLogFields(event *zerolog.Event) *zerolog.Event {
	if j.fields == nil {
		return event
	}
	for k, v := range j.fields {
		event.Any(k, v)
	}
	return event
}

// AddJob 添加或按名更新任务.
// spec 非法 (含空串) 时返回 error, 不停止同名旧任务; 合法后同 spec skip, 不同则先停再挂.
// 并发同名注册须由调用方串行化; 同 spec 复用不替换原 Runner、context、fields、once 或 opts.
func AddJob(ctx context.Context, name, spec string, runner Runner, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, false, nil, opts...)
}

// AddOnceJob 添加单次任务, 只尝试执行一次, 返回错误或 panic 后也自动移除, 不自动重试.
// 同名同 spec 的运行中任务仍按 AddJob 规则复用, 不改变原任务类型.
func AddOnceJob(ctx context.Context, name, spec string, runner Runner, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, true, nil, opts...)
}

// AddJobWithFields 按 AddJob 规则注册任务, 浅拷贝 fields 后附加到执行错误日志.
func AddJobWithFields(ctx context.Context, name, spec string, runner Runner, fields map[string]any, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, false, fields, opts...)
}

// AddOnceJobWithFields 按 AddOnceJob 规则注册单次任务, 浅拷贝 fields 后附加到执行错误日志.
func AddOnceJobWithFields(ctx context.Context, name, spec string, runner Runner, fields map[string]any, opts ...cron.EntryOption) (*Job, error) {
	return addJob(ctx, name, spec, runner, true, fields, opts...)
}

// addJob 是 AddJob / AddOnceJob / *WithFields 的统一入口.
//
// 热加载 Runtime 按单线程调用, 并发同名注册须由调用方串行化. 非法 spec 绝不能先 Stop 同名旧任务,
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

// GetJob 返回查询时该名称的登记对象; 查询之后仍可能被停止或替换.
func GetJob(name string) (*Job, bool) {
	return jobs.Load(name)
}

// StopJob 停止查询时的同名任务, 返回值表示是否找到登记, 不表示等待 Runner 执行结束.
// 与同名替换并发时, 只停止此次查询取得的对象.
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
// 宽限期沿用进程 StartTime, 不按每个 Job 的注册时间重新计算.
func IsRealBlocked() error {
	if time.Since(common.StartTime) > BlockedLimit {
		return ErrJobBlocked
	}
	return nil
}
