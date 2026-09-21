package master

import (
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/config"
)

// TestWatcherStartStopContract 验证无效 watcher 被忽略, 有效 watcher 注册默认 hash 并可删除.
func TestWatcherStartStopContract(t *testing.T) {
	preserveMasterPackageState(t)
	Watcher{}.Start()
	assert.Equal(t, 0, watchers.Size())

	file := filepath.Join(t.TempDir(), "watched.conf")
	assert.Nil(t, os.WriteFile(file, []byte("one"), 0o600))
	w := Watcher{Key: "app", Files: []string{file}, Func: func() {}}
	w.Start()
	stored, ok := watchers.Load("app")
	assert.True(t, ok)
	assert.True(t, stored.HashGenerator != nil)
	wantMD5 := MD5Files(file)
	gotMD5, ok := watcherMD5.Load("app")
	assert.True(t, ok)
	assert.Equal(t, wantMD5, gotMD5)

	w.Stop()
	_, watcherExists := watchers.Load("app")
	_, md5Exists := watcherMD5.Load("app")
	assert.False(t, watcherExists)
	assert.False(t, md5Exists)
}

// TestAppWatcherChangeAndAlways 验证普通 watcher 只在 hash 变化时触发, Always 每轮触发.
func TestAppWatcherChangeAndAlways(t *testing.T) {
	preserveMasterPackageState(t)
	var version atomic.Int64
	changed := make(chan struct{}, 2)
	always := make(chan struct{}, 2)
	Watcher{Key: "changed", Func: func() { changed <- struct{}{} }, HashGenerator: func(...string) string {
		return string(rune('0' + version.Load()))
	}}.Start()
	Watcher{Key: "always", Func: func() { always <- struct{}{} }, Always: true, HashGenerator: func(...string) string { return "stable" }}.Start()

	appWatcher()
	waitMasterSignal(t, always, "always watcher first run")
	assertMasterNoSignal(t, changed, "unchanged watcher")

	version.Store(1)
	appWatcher()
	waitMasterSignal(t, changed, "changed watcher")
	waitMasterSignal(t, always, "always watcher second run")
}

// TestMainWatcherUsesBinaryHash 验证主程序二进制 hash 变化只发送 restart 信号.
func TestMainWatcherUsesBinaryHash(t *testing.T) {
	preserveMasterPackageState(t)
	mainFile = filepath.Join(t.TempDir(), "app")
	assert.Nil(t, os.WriteFile(mainFile, []byte("v1"), 0o600))
	watcherMD5.Store(MainWatcherKey, MD5Files(mainFile))
	assert.False(t, mainWatcher())
	assertMasterNoSignal(t, restartChan, "unchanged binary")

	assert.Nil(t, os.WriteFile(mainFile, []byte("v2"), 0o600))
	assert.True(t, mainWatcher())
	waitMasterSignal(t, restartChan, "changed binary")
}

// TestConfigWatcherBranches 验证配置未变化、加载失败和加载成功三条分支.
func TestConfigWatcherBranches(t *testing.T) {
	prepareMasterConfig(t)
	config.AppConfigBody = nil
	assert.Nil(t, os.WriteFile(config.ConfigFile, []byte(`{"sys_conf":`), 0o600))
	watcherMD5.Store(MainWatcherConfKey, MD5Files(config.ConfigFile))
	assert.True(t, configWatcher())

	assert.Nil(t, os.WriteFile(config.ConfigFile, []byte(`{"sys_conf":`), 0o600))
	watcherMD5.Store(MainWatcherConfKey, "different")
	original := config.Config()
	assert.True(t, configWatcher())
	assert.True(t, config.Config() == original)

	body := []byte(`{"sys_conf":{"watcher_interval":"30s","req_timeout":"1s","deb_version":"2.0.0"}}`)
	assert.Nil(t, os.WriteFile(config.ConfigFile, body, 0o600))
	assert.False(t, configWatcher())
	assert.Equal(t, "2.0.0", config.Config().SYSConf.DebVersion)
	assert.False(t, ConfigModTime.IsZero())
}

// TestConfigWatcherRestoresOldContent 验证失败不推进基线, 恢复原内容也会重新加载并解除安装暂停.
func TestConfigWatcherRestoresOldContent(t *testing.T) {
	prepareMasterConfig(t)
	config.AppConfigBody = nil
	body := []byte(`{"sys_conf":{"deb_version":"2.0","canary_deployment":0}}`)
	assert.Nil(t, os.WriteFile(config.ConfigFile, body, 0o600))
	debInstall = newDebInstaller("test-pkg", func() debTarget {
		cfg := config.Config()
		return debTarget{version: cfg.SYSConf.DebVersion, threshold: cfg.SYSConf.CanaryDeployment}
	})
	assert.False(t, configWatcher())
	baseline, _ := watcherMD5.Load(MainWatcherConfKey)
	assert.Nil(t, os.WriteFile(config.ConfigFile, []byte(`{"sys_conf":`), 0o600))
	assert.True(t, configWatcher())
	got, _ := watcherMD5.Load(MainWatcherConfKey)
	assert.Equal(t, baseline, got)
	debInstall.mu.Lock()
	assert.True(t, debInstall.paused)
	debInstall.mu.Unlock()
	assert.Nil(t, os.WriteFile(config.ConfigFile, body, 0o600))
	assert.False(t, configWatcher())
	assert.False(t, watcherConfigDirty)
	debInstall.mu.Lock()
	assert.False(t, debInstall.paused)
	assert.Equal(t, "2.0", debInstall.target.version)
	debInstall.mu.Unlock()
	assert.True(t, configWatcher())
}

// TestMD5ConfigFilesIncludesAllSources 验证配置 hash 汇总包含主配置、名单、env、额外文件和 NodeInfo.
func TestMD5ConfigFilesIncludesAllSources(t *testing.T) {
	prepareMasterConfig(t)
	extra := filepath.Join(t.TempDir(), "extra.conf")
	node := filepath.Join(t.TempDir(), "node.json")
	assert.Nil(t, os.WriteFile(extra, []byte("extra"), 0o600))
	assert.Nil(t, os.WriteFile(node, []byte("node"), 0o600))
	SetExtraWatcherFiles(extra)
	config.NodeInfoFile = node

	md5, files := MD5ConfigFiles()
	assert.True(t, md5 != "")
	for _, file := range []string{config.ConfigFile, config.WhitelistConfigFile, config.BlacklistConfigFile, extra, node} {
		assert.True(t, slices.Contains(files, file), file)
	}
	for _, envFile := range config.GetEnvFiles() {
		assert.True(t, slices.Contains(files, envFile), envFile)
	}
}

// TestMD5FilesTracksContent 验证文件内容变化会改变组合 hash, 缺失文件仍产生稳定结果.
func TestMD5FilesTracksContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "content.conf")
	assert.Nil(t, os.WriteFile(file, []byte("one"), 0o600))
	first := MD5Files(file)
	assert.Equal(t, first, MD5Files(file))
	assert.Nil(t, os.WriteFile(file, []byte("two"), 0o600))
	assert.True(t, first != MD5Files(file))
	missing := filepath.Join(t.TempDir(), "missing")
	assert.Equal(t, MD5Files(missing), MD5Files(missing))
}

// waitMasterSignal 等待受控 channel 事件, 超时表示 watcher 行为未发生.
func waitMasterSignal[T any](t *testing.T, ch <-chan T, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s", name)
	}
}

// assertMasterNoSignal 在短窗口内确认不应出现的同步事件.
func assertMasterNoSignal[T any](t *testing.T, ch <-chan T, name string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("unexpected signal for %s", name)
	case <-time.After(20 * time.Millisecond):
	}
}
