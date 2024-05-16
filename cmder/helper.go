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

const (
	// 命令执行超时时间默认值
	cmdTimeout = 3 * time.Second
)

var ErrCMDTimeout = errors.New("command execution timed out")

// RunCmd 运行命令, 返回结果和状态
func RunCmd(cmdArgs []string, timeout ...time.Duration) cmd.Status {
	dur := cmdTimeout
	if len(timeout) > 0 {
		dur = timeout[0]
	}

	c := cmd.NewCmd(cmdArgs[0], cmdArgs[1:]...)
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
