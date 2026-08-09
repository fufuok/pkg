package master

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

// prepareMasterConfig 隔离 master 状态并建立自包含配置, 返回可按测试需要修改的配置指针.
func prepareMasterConfig(t *testing.T) *config.MainConf {
	t.Helper()
	preserveMasterPackageState(t)
	oldDebVersion := config.DebVersion
	config.InitTester()
	t.Cleanup(func() {
		config.StopTester()
		config.DebVersion = oldDebVersion
	})
	return config.Config()
}

// TestCanaryBoundary 验证 0、固定 hash 桶边界和 100 三类阈值.
func TestCanaryBoundary(t *testing.T) {
	preserveMasterPackageState(t)
	common.InternalIPv4 = "10.0.0.1"
	common.ExternalIPv4 = "203.0.113.1"
	const (
		version = "1.2.3"
		bucket  = uint64(66)
	)
	assert.False(t, canary(version, 0))
	assert.True(t, canary(version, 100))

	// 固定输入的 hash 桶为 66, 直接区分严格小于与小于等于两种实现.
	assert.False(t, canary(version, bucket))
	assert.True(t, canary(version, bucket+1))
}

// TestCheckUpgradeOrRestartSignals 验证灰度阈值 0 不安装, restart 配置只发送重启信号.
func TestCheckUpgradeOrRestartSignals(t *testing.T) {
	prepareMasterConfig(t)
	config.DebVersion = "1.0.0"
	cfg := config.SYSConf{DebVersion: "2.0.0", CanaryDeployment: 0}
	assert.False(t, checkUpgradeOrRestart(cfg))
	assertMasterNoSignal(t, restartChan, "canary disabled")
	assert.False(t, debInstalling.Load())

	cfg.RestartMain = true
	assert.True(t, checkUpgradeOrRestart(cfg))
	waitMasterSignal(t, restartChan, "restart config")
}

// TestAddonsWithDisabledTimeSync 验证空时间同步配置下 addons 三段生命周期均可安全执行.
func TestAddonsWithDisabledTimeSync(t *testing.T) {
	cfg := prepareMasterConfig(t)
	cfg.SYSConf.TimeSyncType = ""
	a := &addons{}
	assert.Nil(t, a.Start())
	assert.Nil(t, a.Runtime())
	assert.Nil(t, a.Stop())
	name, ch := getClockOffsetChan(context.Background(), time.Second)
	assert.Equal(t, "", name)
	assert.True(t, ch == nil)
}

// TestMainVersionFastPath 验证版本模式打印信息后直接返回, 不进入 Run 和永久信号等待.
func TestMainVersionFastPath(t *testing.T) {
	preserveMasterPackageState(t)
	oldAppName, oldVersion, oldGoVersion, oldCommit := config.AppName, config.Version, config.GoVersion, config.GitCommit
	t.Cleanup(func() {
		config.AppName, config.Version, config.GoVersion, config.GitCommit = oldAppName, oldVersion, oldGoVersion, oldCommit
	})
	config.AppName = "Test.App"
	config.Version = "v1.2.3"
	config.GoVersion = "go1.26"
	config.GitCommit = "abcdef"
	FlagParser = func() { Version = true }

	output := captureMasterStdout(t, Main)
	assert.True(t, strings.Contains(output, ">>> Test.App v1.2.3 go1.26"))
	assert.True(t, strings.Contains(output, ">>> abcdef"))
}

// TestDebVersionEmptyName 验证空包名不会执行 dpkg 命令.
func TestDebVersionEmptyName(t *testing.T) {
	assert.Equal(t, "", DebVersion(""))
}

// captureMasterStdout 在串行测试中捕获少量标准输出并恢复原文件句柄.
func captureMasterStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	writerClosed := false
	defer func() {
		os.Stdout = oldStdout
		_ = reader.Close()
		if !writerClosed {
			_ = writer.Close()
		}
	}()

	fn()
	os.Stdout = oldStdout
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	writerClosed = true
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(body)
}
