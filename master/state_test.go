package master

import (
	"slices"
	"testing"

	"github.com/fufuok/cache/xsync"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
)

// preserveMasterPackageState 隔离 master 测试会修改的注册表、channel 和 watcher 状态.
//
// master 的生命周期对象均为进程级单例. 每个相关测试必须先调用本函数且不得并行;
// 测试使用带缓冲 channel, 以便同步验证 restart/reload 信号而不启动永久 scheduler.
func preserveMasterPackageState(t *testing.T) {
	t.Helper()
	oldDebInstall, oldWatcherConfigDirty := debInstall, watcherConfigDirty
	debInstall, watcherConfigDirty = nil, false
	t.Cleanup(func() {
		if debInstall != nil {
			debInstall.stop()
			<-debInstall.done
		}
		debInstall, watcherConfigDirty = oldDebInstall, oldWatcherConfigDirty
	})

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
	oldPipelineRuntimeErrorEvent := pipelineRuntimeErrorEvent
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
	oldNTPDone := ntpDone
	oldNTPFirstDoneChan := ntpFirstDoneChan
	oldNTPGen := ntpGen
	oldClockOffsetChanOf := clockOffsetChanOf
	oldCommonFuncs := common.Funcs
	oldInternalIPv4 := common.InternalIPv4
	oldExternalIPv4 := common.ExternalIPv4

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
	ntpDone = nil
	ntpFirstDoneChan = make(chan struct{})
	ntpGen = 0
	common.Funcs = xsync.NewMap[string, common.Func]()
	common.InternalIPv4 = ""
	common.ExternalIPv4 = ""

	t.Cleanup(func() {
		ntpMu.Lock()
		cancel := ntpCancel
		done := ntpDone
		ntpMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}
		mu.Lock()
		configPipelines = oldConfigPipelines
		mainPipelines = oldMainPipelines
		remotePipelines = oldRemotePipelines
		mu.Unlock()
		restartChan = oldRestartChan
		reloadChan = oldReloadChan
		pipelineRuntimeErrorEvent = oldPipelineRuntimeErrorEvent
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
		ntpMu.Lock()
		ntpName = oldNTPName
		ntpCancel = oldNTPCancel
		ntpDone = oldNTPDone
		ntpFirstDoneChan = oldNTPFirstDoneChan
		ntpGen = oldNTPGen
		ntpMu.Unlock()
		clockOffsetChanOf = oldClockOffsetChanOf
		common.Funcs = oldCommonFuncs
		common.InternalIPv4 = oldInternalIPv4
		common.ExternalIPv4 = oldExternalIPv4
	})
}

// TestPreserveMasterPackageStateRestoresCommonIP 验证 master 用例不会把 canary 输入泄漏到后续测试.
func TestPreserveMasterPackageStateRestoresCommonIP(t *testing.T) {
	oldInternalIPv4 := common.InternalIPv4
	oldExternalIPv4 := common.ExternalIPv4
	common.InternalIPv4 = "192.0.2.10"
	common.ExternalIPv4 = "198.51.100.20"
	t.Cleanup(func() {
		common.InternalIPv4 = oldInternalIPv4
		common.ExternalIPv4 = oldExternalIPv4
	})

	t.Run("mutate isolated state", func(t *testing.T) {
		preserveMasterPackageState(t)
		assert.Equal(t, "", common.InternalIPv4)
		assert.Equal(t, "", common.ExternalIPv4)
		common.InternalIPv4 = "203.0.113.30"
		common.ExternalIPv4 = "203.0.113.40"
	})

	assert.Equal(t, "192.0.2.10", common.InternalIPv4)
	assert.Equal(t, "198.51.100.20", common.ExternalIPv4)
}
