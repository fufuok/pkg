package service

import (
	"context"
	"sync/atomic"

	"github.com/fufuok/pkgapp/conf"
)

const (
	// heartbeatJobName 是 crontab 任务名. 热更时按这个名字替换任务.
	heartbeatJobName = "pkgapp-heartbeat"
)

type global struct {
	ctx    context.Context
	cancel context.CancelFunc

	ticks      atomic.Uint64
	generation atomic.Uint64
	updates    chan struct{}
}

var g *global

// initGlobal 初始化包级状态和配置订阅, 只在 Start 中执行一次.
func initGlobal() {
	g = &global{}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	if conf.Notifier != nil {
		// 订阅只接收发布事件, 新配置回 conf.App() 读取.
		g.updates = conf.Notifier.Sub(conf.DataConfTopic)
		go g.watchConfig()
	}
}

// watchConfig 记录收到的配置通知次数. TryPub 可能丢通知, 不代表发布总次数.
func (gl *global) watchConfig() {
	for {
		select {
		case <-gl.ctx.Done():
			return
		case _, ok := <-gl.updates:
			if !ok {
				return
			}
			gl.generation.Add(1)
		}
	}
}

// stop 取消业务 context, 通知器由稍后执行的 conf.Stop 释放.
func (gl *global) stop() {
	if gl.cancel != nil {
		gl.cancel()
	}
}

// Ticks 返回本进程已执行的心跳次数.
func Ticks() uint64 {
	if g == nil {
		return 0
	}
	return g.ticks.Load()
}

// Generation 返回本进程收到的配置通知次数, 启动前发布的配置不计数.
func Generation() uint64 {
	if g == nil {
		return 0
	}
	return g.generation.Load()
}

// Context 返回业务包共享的生命周期 context, Stop 时取消.
func Context() context.Context {
	if g == nil {
		return context.Background()
	}
	return g.ctx
}
