//go:build linux

package sysenv

// Linux 发行版常见绝对路径. 安装器和命令封装直接使用, 不在运行时搜索 PATH.
// 非 Linux 构建提供同名占位, 避免调用方再写平台分支.
const (
	BinApt       = "/usr/bin/apt"
	BinAptConfig = "/usr/bin/apt-config"
	BinAptGet    = "/usr/bin/apt-get"
	BinBash      = "/bin/bash"
	BinDash      = "/bin/dash"
	BinDpkg      = "/usr/bin/dpkg"
	BinDpkgDeb   = "/usr/bin/dpkg-deb"
	BinDpkgQuery = "/usr/bin/dpkg-query"
	BinNSLookup  = "/usr/bin/nslookup"
	BinSh        = "/bin/sh"
	BinSleep     = "/bin/sleep"
	BinSystemctl = "/usr/bin/systemctl"
)
