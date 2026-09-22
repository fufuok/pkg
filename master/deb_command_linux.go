//go:build linux

package master

import (
	"os/exec"
	"syscall"
)

// prepareDebCommand 让超时取消作用于 apt/dpkg 启动的整个进程组.
// 只杀直接子进程时, 其子孙仍可能持有 dpkg 锁并阻塞其他安装.
func prepareDebCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if err != nil && err != syscall.ESRCH {
			return err
		}
		return nil
	}
}
