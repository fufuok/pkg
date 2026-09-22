//go:build linux

package master

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/sysenv"
)

// TestDebLinuxCommands 使用真实命令验证环境覆盖、有界输出及查询超时后的进程回收.
func TestDebLinuxCommands(t *testing.T) {
	t.Setenv("LC_ALL", "not-a-locale")
	t.Setenv("DEBIAN_FRONTEND", "interactive")
	env := runDebCommand(time.Second, sysenv.BinSh, "-c", `printf '%s|%s' "$LC_ALL" "$DEBIAN_FRONTEND"`)
	if env.err != nil || env.output != "C|noninteractive" {
		t.Fatalf("environment: %+v", env)
	}
	large := runDebCommand(time.Second, sysenv.BinSh, "-c", "head -c 100000 /dev/zero; head -c 100000 /dev/zero >&2")
	if large.err != nil || len(large.output) != debOutputLimit+len("\n[output truncated]") {
		t.Fatalf("output not bounded: %d %v", len(large.output), large.err)
	}
	timed := runDebCommand(10*time.Millisecond, sysenv.BinSleep, "10")
	if !errors.Is(timed.err, context.DeadlineExceeded) || timed.exit != -1 {
		t.Fatalf("query timeout: %+v", timed)
	}
	done := runDebCommand(0, sysenv.BinSleep, "0.01")
	if done.err != nil || done.exit != 0 {
		t.Fatalf("wait command: %+v", done)
	}
}

// TestDebLinuxVersions 验证真实Debian排序和缺包处理, 不写入宿主数据库.
func TestDebLinuxVersions(t *testing.T) {
	if _, err := os.Stat(debDpkg); err != nil {
		t.Skip("dpkg is not installed")
	}
	for _, tc := range []struct {
		a, op, b string
		exit     int
	}{
		{"1:1.0", "gt", "9.0", 0},
		{"1.10", "gt", "1.9", 0},
		{"1.0~rc1", "lt", "1.0", 0},
		{"1.0-2", "gt", "1.0-1", 0},
		{"1.0", "eq", "1.0-0", 0},
		{"1.0", "gt", "2.0", 1},
	} {
		r := runDebCommand(debQueryTimeout, debDpkg, "--compare-versions", tc.a, tc.op, tc.b)
		if r.exit != tc.exit {
			t.Fatalf("%+v: %+v", tc, r)
		}
	}
	if r := runDebCommand(debQueryTimeout, debDpkg, "--validate-version", "1.0;rm"); r.err == nil {
		t.Fatal("dpkg accepted invalid version")
	}
	admin := t.TempDir()
	if err := os.Mkdir(filepath.Join(admin, "updates"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeDebFixture(t, filepath.Join(admin, "status"), "")
	run := func(d time.Duration, args ...string) debCommandResult {
		return runDebCommand(d, append([]string{args[0], "--admindir=" + admin}, args[1:]...)...)
	}
	v, err := queryDebVersion(run, "test-pkg")
	if v != "" || err != nil {
		t.Fatalf("missing package: %q %v", v, err)
	}
	writeDebFixture(t, filepath.Join(admin, "status"), debFixtureStatus("installed", "1:1.0+build~rc1-2"))
	v, err = queryDebVersion(run, "test-pkg")
	if v != "1:1.0+build~rc1-2" || err != nil {
		t.Fatalf("query full version: %q %v", v, err)
	}
}

// TestDebLinuxEquivalentRetry 用真实dpkg验证语义相等但字面不同的版本仍允许同轮失败补试.
func TestDebLinuxEquivalentRetry(t *testing.T) {
	if _, err := os.Stat(debDpkg); err != nil {
		t.Skip("dpkg is not installed")
	}
	target := debTarget{version: "1.0-0", threshold: 100}
	u := newDebInstaller("test-pkg", func() debTarget { return target })
	u.target, u.round = target, 1
	u.run = func(d time.Duration, args ...string) debCommandResult {
		if args[0] == debDpkgQuery {
			return debCommandResult{output: "installed\t1.0\n"}
		}
		return runDebCommand(d, args...)
	}
	allowed, err := u.gate(target, 1, true)
	if err != nil || !allowed {
		t.Fatalf("equivalent retry rejected: allowed=%v err=%v", allowed, err)
	}
	allowed, err = u.gate(target, 1, false)
	if err != nil || allowed {
		t.Fatalf("first equivalent install should skip: allowed=%v err=%v", allowed, err)
	}
}

// TestDebAPTFixture 显式开启时在独立APT目录和dpkg根中安装专用测试包, 绝不安装宿主包.
func TestDebAPTFixture(t *testing.T) {
	if os.Getenv("PKG_DEB_APT_TEST") != "1" {
		t.Skip("set PKG_DEB_APT_TEST=1 for isolated APT integration")
	}
	if err := debToolsReady(); err != nil {
		t.Fatal(err)
	}
	before := debHostFingerprints(t)
	t.Cleanup(func() {
		if after := debHostFingerprints(t); !reflect.DeepEqual(before, after) {
			t.Error("host package database or logs changed")
		}
	})
	base := t.TempDir()
	root := filepath.Join(base, "root")
	admin := filepath.Join(root, "var/lib/dpkg")
	for _, path := range []string{"state/lists/partial", "cache/archives/partial", "log", "etc/parts", "etc/sources", "etc/preferences", "repo", "root/var/lib/dpkg/info", "root/var/lib/dpkg/updates", "root/bin"} {
		if err := os.MkdirAll(filepath.Join(base, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dependencies, err := exec.Command("ldd", sysenv.BinDash).Output()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{sysenv.BinDash}
	for _, p := range strings.Fields(string(dependencies)) {
		if strings.HasPrefix(p, "/") {
			paths = append(paths, p)
		}
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(root, strings.TrimPrefix(p, "/"))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("dash", filepath.Join(root, "bin/sh")); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.0", "2.0"} {
		pkg := filepath.Join(base, "package-"+version)
		writeDebFixture(t, filepath.Join(pkg, "DEBIAN/control"), fmt.Sprintf("Package: test-pkg\nVersion: %s\nArchitecture: all\nMaintainer: Fixture <fixture@example.invalid>\nDescription: isolated fixture\n", version))
		if err := os.Chmod(filepath.Join(pkg, "DEBIAN"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeDebFixture(t, filepath.Join(pkg, "usr/share/test-pkg/marker"), version)
		postinst := filepath.Join(pkg, "DEBIAN/postinst")
		writeDebFixture(t, postinst, "#!/bin/sh\nif [ -e /fail ]; then exit 1; fi\nprintf 'configured\\n' >> /fixture.log\n")
		if err := os.Chmod(postinst, 0o755); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(sysenv.BinDpkgDeb, "--build", pkg, filepath.Join(base, "repo/test-pkg_"+version+"_all.deb")).CombinedOutput()
		if err != nil {
			t.Fatalf("build package: %v %s", err, output)
		}
	}
	scan := exec.Command("dpkg-scanpackages", "--multiversion", ".", "/dev/null")
	scan.Dir = filepath.Join(base, "repo")
	index, err := scan.Output()
	if err != nil {
		t.Fatal(err)
	}
	writeDebFixture(t, filepath.Join(base, "repo/Packages"), string(index))
	writeDebFixture(t, filepath.Join(admin, "status"), debFixtureStatus("installed", "1.0"))
	writeDebFixture(t, filepath.Join(admin, "info/test-pkg.list"), "")
	for _, p := range []string{"etc/empty.conf", "etc/empty.preferences"} {
		writeDebFixture(t, filepath.Join(base, p), "")
	}
	// trusted只授权临时本地fixture, 生产argv不放宽签名. 另加一个不存在的源制造update失败.
	writeDebFixture(t, filepath.Join(base, "etc/sources.list"), "deb [trusted=yes] file:"+filepath.Join(base, "repo")+" ./\ndeb file:"+filepath.Join(base, "missing")+" ./\n")
	settings := map[string]string{
		"Dir": base, "Dir::State": base + "/state", "Dir::State::status": admin + "/status", "Dir::State::lists": base + "/state/lists",
		"Dir::Cache": base + "/cache", "Dir::Cache::archives": base + "/cache/archives", "Dir::Log": base + "/log",
		"Dir::Etc": base + "/etc", "Dir::Etc::main": base + "/etc/empty.conf", "Dir::Etc::parts": base + "/etc/parts",
		"Dir::Etc::sourcelist": base + "/etc/sources.list", "Dir::Etc::sourceparts": base + "/etc/sources",
		"Dir::Etc::preferences": base + "/etc/empty.preferences", "Dir::Etc::preferencesparts": base + "/etc/preferences",
		"APT::Sandbox::User": "root", "APT::Get::allow-downgrades": "true", "APT::Get::force-yes": "true",
	}
	var conf strings.Builder
	for k, v := range settings {
		fmt.Fprintf(&conf, "%s %q;\n", k, v)
	}
	fmt.Fprintf(&conf, "Dpkg::Options { %q; %q; %q; };\n", "--root="+root, "--admindir="+admin, "--log="+base+"/log/dpkg.log")
	writeDebFixture(t, base+"/apt.conf", conf.String())
	t.Setenv("APT_CONFIG", base+"/apt.conf")
	resolved := runDebCommand(debQueryTimeout, sysenv.BinAptConfig, "dump")
	if resolved.err != nil {
		t.Fatal(resolved.failure("resolve fixture"))
	}
	for k, v := range settings {
		if !strings.Contains(resolved.output, fmt.Sprintf("%s %q;", k, v)) {
			t.Fatalf("fixture path/config not resolved: %s", k)
		}
	}
	if strings.Contains(resolved.output, "Pre-Invoke") || strings.Contains(resolved.output, "Post-Invoke") || strings.Contains(resolved.output, "Pre-Install-Pkgs") {
		t.Fatal("inherited host APT hooks")
	}
	for range 2 {
		r := runDebCommand(0, debAPTArgs("update", "")...)
		if r.exit != 100 {
			t.Fatalf("expected broken source: %+v", r)
		}
	}
	// 使用生产argv完成实际升级, 证明update失败不代表目标不可安装.
	installed := runDebCommand(0, debAPTArgs("install", "test-pkg=2.0")...)
	if installed.err != nil {
		t.Fatal(installed.failure("isolated install"))
	}
	t.Log("Two failed updates followed by successful exact install")
	// 明确覆盖主机允许降级与force-yes, 原生APT必须拒绝回退.
	downgrade := runDebCommand(0, debAPTArgs("install", "test-pkg=1.0")...)
	if downgrade.err == nil || !strings.Contains(downgrade.output, "--allow-downgrades") {
		t.Fatalf("downgrade protection: %+v", downgrade)
	}
	// 模拟数字更新日志残留, 只运行隔离root中的configure再重试.
	writeDebFixture(t, admin+"/updates/0000", debFixtureStatus("unpacked", "2.0"))
	interrupted := runDebCommand(0, debAPTArgs("install", "test-pkg=2.0")...)
	if interrupted.err == nil || !strings.Contains(interrupted.output, "dpkg was interrupted") || !strings.Contains(interrupted.output, "dpkg --configure -a") {
		t.Fatalf("interrupt diagnosis: %+v", interrupted)
	}
	configured := runDebCommand(0, debDpkg, "--root="+root, "--admindir="+admin, "--log="+base+"/log/dpkg.log", "--force-confdef", "--force-confold", "--configure", "-a")
	if configured.err != nil {
		t.Fatal(configured.failure("isolated configure"))
	}
	retried := runDebCommand(0, debAPTArgs("install", "test-pkg=2.0")...)
	if retried.err != nil {
		t.Fatal(retried.failure("isolated retry"))
	}
	t.Log("Interrupted dpkg recovered with one configure, exact install succeeded; downgrade rejected")
}

// debFixtureStatus 构造独立包数据库记录, 不读取或修改宿主包名.
func debFixtureStatus(status, version string) string {
	return fmt.Sprintf("Package: test-pkg\nStatus: install ok %s\nPriority: optional\nSection: misc\nArchitecture: all\nVersion: %s\nMaintainer: Fixture <fixture@example.invalid>\nDescription: isolated fixture\n\n", status, version)
}

// writeDebFixture 只写调用者创建的临时树, 目录和文件使用最小访问权限.
func writeDebFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// debHostFingerprints 为隔离测试验证宿主数据库、数字日志及包管理日志没有改变.
func debHostFingerprints(t *testing.T) map[string]string {
	t.Helper()
	paths := []string{"/var/lib/dpkg/status", "/var/log/dpkg.log", "/var/log/apt/history.log", "/var/log/apt/term.log"}
	updates, err := filepath.Glob("/var/lib/dpkg/updates/*")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, updates...)
	result := make(map[string]string, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			result[p] = "missing"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		result[p] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return result
}
