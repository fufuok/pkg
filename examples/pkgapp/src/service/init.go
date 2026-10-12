package service

import (
	"github.com/fufuok/pkg/crontab"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/sysenv"
)

// M 是业务生命周期模块, Start 和 Stop 每个进程各执行一次.
type M struct{}

// Start 初始化业务状态.
func (m *M) Start() error {
	initGlobal()
	logRuntime()
	return initHeartbeat()
}

// Runtime 按任务名替换心跳.
func (m *M) Runtime() error {
	return initHeartbeat()
}

// Stop 停止后续调度并取消业务 context, 已运行的任务自行响应取消.
func (m *M) Stop() error {
	crontab.StopJob(heartbeatJobName)
	if g != nil {
		g.stop()
	}
	return nil
}

// logRuntime 记录当前平台的安装器路径.
func logRuntime() {
	// 非 Linux 只有 unknown 占位, 调用前判断 sysenv.IsLinux.
	event := logger.Info().
		Bool("linux", sysenv.IsLinux()).
		Bool("windows", sysenv.IsWindows()).
		Str("systemctl", sysenv.BinSystemctl)
	if sysenv.IsLinux() {
		event.Str("dpkg", sysenv.BinDpkg)
	}
	event.Msg("Service runtime initialized")
}
