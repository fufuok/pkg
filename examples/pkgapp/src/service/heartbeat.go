package service

import (
	"context"
	"fmt"

	"github.com/fufuok/pkg/crontab"

	"github.com/fufuok/pkgapp/conf"
)

type heartbeat struct{}

// initHeartbeat 按任务名注册或替换心跳, 调度错误交给 master 处理.
func initHeartbeat() error {
	_, err := crontab.AddJob(Context(), heartbeatJobName, conf.App().HeartbeatSpecOrDefault(), &heartbeat{})
	if err != nil {
		return fmt.Errorf("init heartbeat: %w", err)
	}
	return nil
}

// Run 执行一次心跳.
func (h *heartbeat) Run(ctx context.Context) error {
	// 已进入 Run 的任务不会被调度器强制中断.
	if err := ctx.Err(); err != nil {
		return err
	}
	g.ticks.Add(1)
	return nil
}
