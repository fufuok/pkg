package xdaemon

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/fufuok/pkg/assert"
)

const (
	xdaemonHelperEnv    = "PKG_XDAEMON_HELPER"
	xdaemonHelperStdout = "xdaemon helper stdout"
	xdaemonHelperStderr = "xdaemon helper stderr"
)

// TestNewDaemon 验证构造器保留日志路径并设置稳定的重启默认值.
func TestNewDaemon(t *testing.T) {
	daemon := NewDaemon("daemon.log")
	assert.NotNil(t, daemon)
	assert.Equal(t, "daemon.log", daemon.LogFile)
	assert.Equal(t, 0, daemon.MaxCount)
	assert.Equal(t, 3, daemon.MaxError)
	assert.Equal(t, int64(10), daemon.MinExitTime)
}

// TestNewSysProcAttr 验证当前平台启用后台子进程所需的系统属性.
// syscall.SysProcAttr 的字段随平台变化, 因此通过反射检查当前构建目标对应字段.
func TestNewSysProcAttr(t *testing.T) {
	attr := NewSysProcAttr()
	assert.NotNil(t, attr)

	fieldName := "Setsid"
	if runtime.GOOS == "windows" {
		fieldName = "HideWindow"
	}
	field := reflect.ValueOf(attr).Elem().FieldByName(fieldName)
	assert.True(t, field.IsValid(), "SysProcAttr must expose %s", fieldName)
	assert.Equal(t, reflect.Bool, field.Kind())
	assert.True(t, field.Bool(), "SysProcAttr.%s must be enabled", fieldName)
}

// TestStartProcWithLog 使用真实测试二进制启动子进程, 验证 stdout 和 stderr
// 会追加到同一日志文件. 子进程必须等待结束且父进程文件句柄必须显式关闭.
func TestStartProcWithLog(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "daemon.log")
	cmd, err := startProc(xdaemonHelperArgs(), xdaemonHelperEnvironment(), logFile)
	assert.Nil(t, err)
	assert.NotNil(t, cmd)

	waitErr := cmd.Wait()
	logWriter, ok := cmd.Stdout.(*os.File)
	var closeErr error
	if ok {
		closeErr = logWriter.Close()
	}
	assert.Nil(t, waitErr)
	assert.True(t, ok, "startProc must keep the opened log file on stdout")
	assert.Equal(t, cmd.Stdout, cmd.Stderr)
	assert.Nil(t, closeErr)
	assert.NotNil(t, cmd.ProcessState)
	assert.True(t, cmd.ProcessState.Success())

	output, err := os.ReadFile(logFile)
	assert.Nil(t, err)
	assert.True(t, strings.Contains(string(output), xdaemonHelperStdout))
	assert.True(t, strings.Contains(string(output), xdaemonHelperStderr))
}

// TestStartProcWithoutLog 验证空日志路径仍会真实启动并等待子进程,
// 同时保持命令的 stdout 和 stderr 未配置.
func TestStartProcWithoutLog(t *testing.T) {
	cmd, err := startProc(xdaemonHelperArgs(), xdaemonHelperEnvironment(), "")
	assert.Nil(t, err)
	assert.NotNil(t, cmd)
	assert.Nil(t, cmd.Stdout)
	assert.Nil(t, cmd.Stderr)
	assert.Nil(t, cmd.Wait())
	assert.NotNil(t, cmd.ProcessState)
	assert.True(t, cmd.ProcessState.Success())
}

// TestStartProcErrors 验证日志文件无法打开和二进制不存在时返回明确错误,
// 且不会向调用方返回未成功启动的命令.
func TestStartProcErrors(t *testing.T) {
	t.Run("log path is directory", func(t *testing.T) {
		cmd, err := startProc(xdaemonHelperArgs(), xdaemonHelperEnvironment(), t.TempDir())
		assert.Nil(t, cmd)
		assert.NotNil(t, err)
	})

	t.Run("binary does not exist", func(t *testing.T) {
		missingBinary := filepath.Join(t.TempDir(), "missing-binary")
		cmd, err := startProc([]string{missingBinary}, os.Environ(), "")
		assert.Nil(t, cmd)
		assert.NotNil(t, err)
	})
}

// TestBackgroundChildRole 验证环境层级覆盖当前调用层级时直接识别为子角色,
// 不继续创建进程, 也不触发 isExit 分支.
func TestBackgroundChildRole(t *testing.T) {
	resetRunIndex(t)
	t.Setenv(EnvName, "1")

	cmd, err := Background("", false)
	assert.Nil(t, err)
	assert.Nil(t, cmd)
	assert.Equal(t, 1, runIdx)
}

// TestBackgroundStartError 验证非法层级回退为父角色后, 日志打开错误会原样返回.
// 使用目录作为日志路径可以在 Windows 和 Unix 上稳定失败, 且不会启动子进程.
func TestBackgroundStartError(t *testing.T) {
	resetRunIndex(t)
	t.Setenv(EnvName, "invalid")

	cmd, err := Background(t.TempDir(), false)
	assert.Nil(t, cmd)
	assert.NotNil(t, err)
	assert.Equal(t, 1, runIdx)
}

// TestRunChildRole 验证 daemon 子角色连续穿过两层 Background 后返回业务进程.
// 环境层级固定为 2, 因此本路径不会创建进程或执行 os.Exit.
func TestRunChildRole(t *testing.T) {
	resetRunIndex(t)
	t.Setenv(EnvName, "2")

	NewDaemon("").Run()
	assert.Equal(t, 2, runIdx)
}

// TestXDaemonHelperProcess 是 startProc 测试的受控子进程入口.
// 没有显式环境标记时立即返回, 避免普通测试运行误写标准输出.
func TestXDaemonHelperProcess(t *testing.T) {
	if os.Getenv(xdaemonHelperEnv) != "1" {
		return
	}
	_, err := fmt.Fprintln(os.Stdout, xdaemonHelperStdout)
	assert.Nil(t, err)
	_, err = fmt.Fprintln(os.Stderr, xdaemonHelperStderr)
	assert.Nil(t, err)
}

// resetRunIndex 隔离包级调用层级, 并在测试结束时恢复原值.
// 调用方不得并行运行, 否则会与 Background 的全局计数语义产生竞争.
func resetRunIndex(t *testing.T) {
	t.Helper()
	original := runIdx
	runIdx = 0
	t.Cleanup(func() {
		runIdx = original
	})
}

// xdaemonHelperArgs 返回只运行受控 helper 用例的测试二进制参数.
func xdaemonHelperArgs() []string {
	return []string{os.Args[0], "-test.run=^TestXDaemonHelperProcess$"}
}

// xdaemonHelperEnvironment 返回触发 helper 输出的独立环境副本.
func xdaemonHelperEnvironment() []string {
	return append(os.Environ(), xdaemonHelperEnv+"=1")
}
