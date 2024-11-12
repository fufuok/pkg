package sysenv

import (
	"runtime"
)

// IsLinux 检查当前操作系统是否为 Linux
func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// IsWindows 检查当前操作系统是否为 Windows
func IsWindows() bool {
	return runtime.GOOS == "windows"
}
