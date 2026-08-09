package master

import (
	"slices"
	"testing"

	"github.com/fufuok/cache/xsync"

	"github.com/fufuok/pkg/common"
)

// preserveMasterPackageState 隔离 master 测试会修改的注册表、channel 和 watcher 状态.
//
// master 的生命周期对象均为进程级单例. 每个相关测试必须先调用本函数且不得并行;
// 测试使用带缓冲 channel, 以便同步验证 restart/reload 信号而不启动永久 scheduler.
func preserveMasterPackageState(t *testing.T) {
	t.Helper()

	mu.Lock()
	oldConfigPipelines := slices.Clone(configPipelines)
	oldMainPipelines := slices.Clone(mainPipelines)
	oldRemotePipelines := slices.Clone(remotePipelines)
	configPipelines = nil
	mainPipelines = nil
	remotePipelines = nil
	mu.Unlock()

	oldRestartChan := restartChan
	oldReloadChan := reloadChan
	oldConfigModTime := ConfigModTime
	oldConfigLoadTime := ConfigLoadTime
	oldWatchers := watchers
	oldWatcherMD5 := watcherMD5
	oldIsRuntime := isRuntime
	oldMainFile := mainFile
	oldExtraWatcherFiles := slices.Clone(extraWatcherFiles)
	oldFlagParser := FlagParser
	oldDaemon := Daemon
	oldVersion := Version
	oldNTPName := ntpName
	oldNTPCancel := ntpCancel
	oldNTPFirstDoneChan := ntpFirstDoneChan
	oldDebInstalling := debInstalling.Load()
	oldCommonFuncs := common.Funcs

	restartChan = make(chan bool, 1)
	reloadChan = make(chan bool, 1)
	watchers = xsync.NewMap[string, Watcher]()
	watcherMD5 = xsync.NewMap[string, string]()
	isRuntime = false
	mainFile = ""
	extraWatcherFiles = nil
	Daemon = false
	Version = false
	ntpName = ""
	ntpCancel = nil
	ntpFirstDoneChan = make(chan struct{})
	debInstalling.Store(false)
	common.Funcs = xsync.NewMap[string, common.Func]()

	t.Cleanup(func() {
		if ntpCancel != nil {
			ntpCancel()
		}
		mu.Lock()
		configPipelines = oldConfigPipelines
		mainPipelines = oldMainPipelines
		remotePipelines = oldRemotePipelines
		mu.Unlock()
		restartChan = oldRestartChan
		reloadChan = oldReloadChan
		ConfigModTime = oldConfigModTime
		ConfigLoadTime = oldConfigLoadTime
		watchers = oldWatchers
		watcherMD5 = oldWatcherMD5
		isRuntime = oldIsRuntime
		mainFile = oldMainFile
		extraWatcherFiles = oldExtraWatcherFiles
		FlagParser = oldFlagParser
		Daemon = oldDaemon
		Version = oldVersion
		ntpName = oldNTPName
		ntpCancel = oldNTPCancel
		ntpFirstDoneChan = oldNTPFirstDoneChan
		debInstalling.Store(oldDebInstalling)
		common.Funcs = oldCommonFuncs
	})
}
