package sysenv

import "testing"

// TestBinaryPathConstants 固定已使用和安装器依赖的系统路径, 防止平台文件漏改同名常量.
func TestBinaryPathConstants(t *testing.T) {
	if IsLinux() {
		// 按常量名保存期望值. 非 Linux 的多个旧常量同为 unknown, 不能把路径值当作 map key.
		checks := map[string]string{
			"BinApt": "/usr/bin/apt", "BinAptConfig": "/usr/bin/apt-config", "BinAptGet": "/usr/bin/apt-get",
			"BinBash": "/bin/bash", "BinDash": "/bin/dash", "BinDpkg": "/usr/bin/dpkg", "BinDpkgDeb": "/usr/bin/dpkg-deb",
			"BinDpkgQuery": "/usr/bin/dpkg-query", "BinNSLookup": "/usr/bin/nslookup", "BinSh": "/bin/sh",
			"BinSleep": "/bin/sleep", "BinSystemctl": "/usr/bin/systemctl",
		}
		got := map[string]string{
			"BinApt": BinApt, "BinAptConfig": BinAptConfig, "BinAptGet": BinAptGet,
			"BinBash": BinBash, "BinDash": BinDash, "BinDpkg": BinDpkg, "BinDpkgDeb": BinDpkgDeb,
			"BinDpkgQuery": BinDpkgQuery, "BinNSLookup": BinNSLookup, "BinSh": BinSh,
			"BinSleep": BinSleep, "BinSystemctl": BinSystemctl,
		}
		for name, want := range checks {
			if got[name] != want {
				t.Fatalf("%s=%q, want %q", name, got[name], want)
			}
		}
		return
	}
	// 既有五个常量的 Windows 值已被外部使用, 不能改成带后缀的新占位.
	for _, path := range []string{BinApt, BinBash, BinDpkg, BinNSLookup, BinSystemctl} {
		if path != "unknown" {
			t.Fatalf("legacy placeholder changed: %q", path)
		}
	}
	seen := map[string]bool{"unknown": true}
	for _, path := range []string{BinAptConfig, BinAptGet, BinDash, BinDpkgDeb, BinDpkgQuery, BinSh, BinSleep} {
		if seen[path] || len(path) <= len("unknown-") || path[:len("unknown-")] != "unknown-" {
			t.Fatalf("placeholder %q is not a distinct sentinel", path)
		}
		seen[path] = true
	}
}
