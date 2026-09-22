//go:build !linux

package master

import "os/exec"

// prepareDebCommand 在非 Linux 上不创建进程组.
// 安装器本身要求 Linux, 此分支只保证开发和测试构建可编译.
func prepareDebCommand(_ *exec.Cmd) {}
