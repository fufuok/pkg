package cmder

import (
	"fmt"
	"strings"
	"time"

	"github.com/fufuok/pkg/sysenv"
)

var (
	// DefaultShellTimeout Shell 执行默认超时时间
	DefaultShellTimeout = 10 * time.Second

	// BashCmd 主命令绝对路径
	BashCmd = []string{sysenv.BinBash}
)

// RunShell 运行 Shell 脚本返回是否执行成功
func RunShell(sh string, args ...string) (ok bool) {
	_, _, ok = RunShellTimeoutWithResult(sh, DefaultShellTimeout, args...)
	return
}

// RunShellTimeout 运行 Shell 脚本返回是否执行成功
func RunShellTimeout(sh string, timeout time.Duration, args ...string) (ok bool) {
	_, _, ok = RunShellTimeoutWithResult(sh, timeout, args...)
	return
}

// RunShellWithResult 运行 Shell 脚本并返回输出结果
func RunShellWithResult(sh string, args ...string) (stdout, stderr string, ok bool) {
	stdout, stderr, ok = RunShellTimeoutWithResult(sh, DefaultShellTimeout, args...)
	return
}

// RunShellTimeoutWithResult 运行 Shell 脚本并返回标准输出和错误输出, 以及是否执行成功
// 示例命令: /bin/bash /opt/app/script/echo.sh my-app
// 示例调用: RunShellTimeoutWithResult("/opt/app/script/echo.sh", 3*time.Second, "my-app")
func RunShellTimeoutWithResult(sh string, timeout time.Duration, args ...string) (stdout, stderr string, ok bool) {
	cmd := append(BashCmd, sh)
	if len(args) > 0 {
		cmd = append(cmd, args...)
	}

	status := RunCmd(cmd, timeout)
	stdout = strings.Join(status.Stdout, "\n")
	stdout = strings.TrimSpace(stdout)
	stderr = ""
	if status.Error != nil {
		stderr += fmt.Sprintf("error: %v\n", status.Error)
	}
	stderr += strings.Join(status.Stderr, "\n")
	stderr = strings.TrimSpace(stderr)
	ok = status.Exit == 0 && status.Error == nil
	return
}
