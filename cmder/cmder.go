package cmder

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/pools/timerpool"
	"github.com/go-cmd/cmd"
)

var (
	// DefaultCMDTimeout 命令执行超时时间默认值
	DefaultCMDTimeout = 3 * time.Second

	ErrCMDTimeout = errors.New("command execution timed out")
)

// RunCmd 运行命令, 返回结果和状态
func RunCmd(cmdArgs []string, timeout ...time.Duration) cmd.Status {
	opts := cmd.Options{Buffered: true}
	return RunCmdWithOptions(cmdArgs, opts, timeout...)
}

// RunCmdCombinedOutput 运行命令, 合并输出和错误 2>&1
func RunCmdCombinedOutput(cmdArgs []string, timeout ...time.Duration) cmd.Status {
	opts := cmd.Options{CombinedOutput: true}
	return RunCmdWithOptions(cmdArgs, opts, timeout...)
}

func RunCmdWithOptions(cmdArgs []string, opts cmd.Options, timeout ...time.Duration) cmd.Status {
	dur := DefaultCMDTimeout
	if len(timeout) > 0 {
		dur = timeout[0]
	}

	c := cmd.NewCmdOptions(opts, cmdArgs[0], cmdArgs[1:]...)
	timer := timerpool.New(dur)
	defer timerpool.Release(timer)

	select {
	case status := <-c.Start():
		return status
	case t := <-timer.C:
		_ = c.Stop()
		stopTs := t.UnixNano()
		startTs := t.UnixNano() - dur.Nanoseconds()
		return cmd.Status{
			Cmd:      cmdArgs[0],
			Complete: false,
			Exit:     130,
			Error:    ErrCMDTimeout,
			StartTs:  startTs,
			StopTs:   stopTs,
			Runtime:  utils.Round(dur.Seconds(), 2),
		}
	}
}

// RunCmdWithContext 运行命令, 返回结果和状态
func RunCmdWithContext(ctx context.Context, cmdArgs []string) cmd.Status {
	start := time.Now()
	c := cmd.NewCmd(cmdArgs[0], cmdArgs[1:]...)
	select {
	case status := <-c.Start():
		return status
	case <-ctx.Done():
		_ = c.Stop()
		end := time.Now()
		return cmd.Status{
			Cmd:      cmdArgs[0],
			Complete: false,
			Exit:     130,
			Error:    ErrCMDTimeout,
			StartTs:  start.UnixNano(),
			StopTs:   end.UnixNano(),
			Runtime:  utils.Round(end.Sub(start).Seconds(), 2),
		}
	}
}

// CheckBadCmd 检查命令是否包含潜在非法字符
func CheckBadCmd(s string) bool {
	if strings.Contains(s, "&") || strings.Contains(s, "|") || strings.Contains(s, ";") ||
		strings.Contains(s, "$") || strings.Contains(s, "'") || strings.Contains(s, "`") ||
		strings.Contains(s, "(") || strings.Contains(s, ")") || strings.Contains(s, "\"") {
		return true
	}
	return false
}
