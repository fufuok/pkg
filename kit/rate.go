package kit

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils"
)

// RateState 通过计数增长和时间间隔计算速率
type RateState struct {
	lastRate  float64
	lastTime  time.Time
	lastCount uint64
	minSecond float64

	// 轻量级 TryLock
	tryLock atomic.Int32

	// 延迟初始化
	initOnce sync.Once
}

// NewRateState 创建速率计算器, 可选设置最小保护时间间隔, 默认 1 秒
func NewRateState(seconds ...float64) *RateState {
	sec := 1.0
	if len(seconds) > 0 {
		sec = seconds[0]
	}
	r := &RateState{
		minSecond: sec,
	}
	return r
}

// SetMinSecond 设置最小时间间隔 (秒)
//
//go:norace
func (r *RateState) SetMinSecond(second float64) {
	r.minSecond = second
}

// Rate 计算并返回速率(每秒), 返回最近两次触达计算的请求时间间隔之间的计数增长平均速率
// 非实时计算, 可能返回上一轮速率结果
// 非精确计算, 当计数增长突发性很大或请求的时间间隔很长时, 速率结果与实际误差较大
// 若需要得到相对精准的结果, 按指定时间间隔调用 Rate 函数
// -1 表示计数器被重置或无效, 0 表示无变化
func (r *RateState) Rate(count uint64) float64 {
	// 确保零值实例默认 minSecond 值为: 1.0
	r.initOnce.Do(func() {
		if r.minSecond <= 0 {
			r.minSecond = 1.0
		}
	})

	// 异常数据 或 重置计数器
	if count == 0 || count < r.lastCount {
		r.lastCount = 0
		return -1
	}

	// 避免频繁计算, 允许使用旧值
	if !r.tryLock.CompareAndSwap(0, 1) {
		return r.lastRate
	}
	defer r.tryLock.Store(0)

	// 首次调用 或 计数器重置, 重新开始记录状态数据
	now := time.Now()
	if r.lastCount == 0 {
		r.lastRate = 0
		r.lastTime = now
		r.lastCount = count
		return 0
	}

	// 计数未改变时: 不更新时间
	if count == r.lastCount {
		return 0
	}

	// 计算时间差 (秒), 至少达到 n 秒的间隔才计算速率
	sec := now.Sub(r.lastTime).Seconds()
	if sec < r.minSecond {
		return r.lastRate
	}

	// 计算速率 = (当前总数 - 上次总数) / 时间差
	rate := utils.Round(float64(count-r.lastCount)/sec, 2)
	r.lastRate = rate
	r.lastTime = now
	r.lastCount = count
	return rate
}
