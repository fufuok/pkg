//go:build linux

package master

import (
	"os"
	"syscall"
)

// sendBinaryChangeSIGTERM 向当前进程发送SIGTERM, 让master.Run走正常Stop流程.
func sendBinaryChangeSIGTERM() error {
	return syscall.Kill(os.Getpid(), syscall.SIGTERM)
}
