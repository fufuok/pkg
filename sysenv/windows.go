//go:build !linux

package sysenv

// 非 Linux 不提供这些绝对路径, 调用前仍须用 IsLinux 判断.
// 既有常量保持 unknown. 新增常量必须互不相同, 否则安装器按命令身份分支时会变成重复 case.
const (
	BinApt       = "unknown"
	BinAptConfig = "unknown-apt-config"
	BinAptGet    = "unknown-apt-get"
	BinBash      = "unknown"
	BinDash      = "unknown-dash"
	BinDpkg      = "unknown"
	BinDpkgDeb   = "unknown-dpkg-deb"
	BinDpkgQuery = "unknown-dpkg-query"
	BinNSLookup  = "unknown"
	BinSh        = "unknown-sh"
	BinSleep     = "unknown-sleep"
	BinSystemctl = "unknown"
)
