package master

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/crontab"
	"github.com/rs/zerolog"
)

// recordingPipeline 记录生命周期调用, 用于验证顺序和错误后继续执行语义.
type recordingPipeline struct {
	name       string
	events     *[]string
	startErr   error
	runtimeErr error
	stopErr    error
	startHook  func()
}

// Start 记录启动事件并返回预设错误.
func (p *recordingPipeline) Start() error {
	*p.events = append(*p.events, "start:"+p.name)
	if p.startHook != nil {
		p.startHook()
	}
	return p.startErr
}

// Runtime 记录热加载事件并返回预设错误.
func (p *recordingPipeline) Runtime() error {
	*p.events = append(*p.events, "runtime:"+p.name)
	return p.runtimeErr
}

// Stop 记录停止事件并返回预设错误.
func (p *recordingPipeline) Stop() error {
	*p.events = append(*p.events, "stop:"+p.name)
	return p.stopErr
}

// TestPipelineRegistrationMatrix 验证每种注册函数只接受其支持的 Stage, 且读取返回快照.
func TestPipelineRegistrationMatrix(t *testing.T) {
	preserveMasterPackageState(t)
	var events []string
	configPipeline := &recordingPipeline{name: "config", events: &events}
	mainPipeline := &recordingPipeline{name: "main", events: &events}
	remote := func(context.Context) {}

	Register(ConfigStage, configPipeline)
	Register(MainStage, mainPipeline)
	RegisterWithContext(RemoteStage, remote)

	configSnapshot := getPipelines(ConfigStage)
	mainSnapshot := getPipelines(MainStage)
	remoteSnapshot := getPipelinesWithContext(RemoteStage)
	assert.Equal(t, 1, len(configSnapshot))
	assert.Equal(t, 1, len(mainSnapshot))
	assert.Equal(t, 1, len(remoteSnapshot))

	configSnapshot[0] = mainPipeline
	remoteSnapshot[0] = nil
	assert.True(t, getPipelines(ConfigStage)[0] == configPipeline)
	assert.True(t, getPipelinesWithContext(RemoteStage)[0] != nil)
}

// TestRegisterPipelineFrameworkOrder 冻结框架 Pipeline 与业务 Pipeline 的组合顺序.
func TestRegisterPipelineFrameworkOrder(t *testing.T) {
	preserveMasterPackageState(t)
	var events []string
	businessConfig := &recordingPipeline{name: "business-config", events: &events}
	businessMain := &recordingPipeline{name: "business-main", events: &events}
	Register(ConfigStage, businessConfig)
	Register(MainStage, businessMain)

	registerPipeline()
	configItems := getPipelines(ConfigStage)
	mainItems := getPipelines(MainStage)
	assert.Equal(t, 3, len(configItems))
	assert.Equal(t, 3, len(mainItems))
	assert.True(t, reflect.TypeOf(configItems[0]) == reflect.TypeFor[*config.M]())
	assert.True(t, reflect.TypeOf(configItems[1]) == reflect.TypeFor[*common.M]())
	assert.True(t, configItems[2] == businessConfig)
	assert.True(t, reflect.TypeOf(mainItems[0]) == reflect.TypeFor[*crontab.M]())
	assert.True(t, mainItems[1] == businessMain)
	assert.True(t, reflect.TypeOf(mainItems[2]) == reflect.TypeFor[*addons]())
}

// TestPipelineExecutionOrder 验证启动、两类 Runtime 和整体逆序 Stop 的真实执行顺序.
func TestPipelineExecutionOrder(t *testing.T) {
	preserveMasterPackageState(t)
	events := make([]string, 0, 12)
	c1 := &recordingPipeline{name: "c1", events: &events}
	c2 := &recordingPipeline{name: "c2", events: &events, runtimeErr: errors.New("runtime config")}
	m1 := &recordingPipeline{name: "m1", events: &events, runtimeErr: errors.New("runtime main")}
	m2 := &recordingPipeline{name: "m2", events: &events}
	configPipelines = []Pipeline{c1, c2}
	mainPipelines = []Pipeline{m1, m2}

	startConfigPipeline()
	runtimeConfigPipeline()
	runtimePipeline()
	stopPipeline()

	want := []string{
		"start:c1", "start:c2",
		"runtime:c1", "runtime:c2",
		"runtime:m1", "runtime:m2",
		"stop:m2", "stop:m1", "stop:c2", "stop:c1",
	}
	assert.Equal(t, want, events)
}

// TestPipelineRuntimeErrorsAreReported 验证两类 Runtime 错误携带固定消息上报, 且不会中断后续 Pipeline.
func TestPipelineRuntimeErrorsAreReported(t *testing.T) {
	preserveMasterPackageState(t)
	var reports bytes.Buffer
	testLogger := zerolog.New(&reports)
	pipelineRuntimeErrorEvent = func() *zerolog.Event {
		return testLogger.Error()
	}
	configErr := errors.New("expected config runtime error")
	mainErr := errors.New("expected main runtime error")
	var events []string
	configPipelines = []Pipeline{
		&recordingPipeline{name: "config", events: &events, runtimeErr: configErr},
		&recordingPipeline{name: "config-after-error", events: &events},
	}
	mainPipelines = []Pipeline{
		&recordingPipeline{name: "main", events: &events, runtimeErr: mainErr},
		&recordingPipeline{name: "main-after-error", events: &events},
	}
	runtimeConfigPipeline()
	runtimePipeline()

	assert.Equal(t, []string{
		"runtime:config", "runtime:config-after-error",
		"runtime:main", "runtime:main-after-error",
	}, events)
	output := reports.String()
	for _, want := range []string{
		"Runtime config pipeline failed",
		"expected config runtime error",
		"Runtime main pipeline failed",
		"expected main runtime error",
	} {
		assert.Contains(t, want, output)
	}
}

// TestStartPipelineExecutionOrder 使用子进程执行真实 startPipeline, 并在最后一个 Start 内退出以避开永久 scheduler.
func TestStartPipelineExecutionOrder(t *testing.T) {
	output, err := runMasterSubprocess(t, "TestStartPipelineExecutionOrderHelper", "PKG_MASTER_START_ORDER_HELPER")
	assert.True(t, err != nil)
	var exitErr *exec.ExitError
	assert.True(t, errors.As(err, &exitErr))
	assert.Contains(t, "PKG_MASTER_MAIN_ORDER=start:crontab,start:business,start:addons", string(output))
	assert.Contains(t, "Failed to initialize main: stop before scheduler", string(output))
}

// TestStartPipelineExecutionOrderHelper 在 addons 位置记录最终顺序后退出, 防止 startPipeline 启动 scheduler.
func TestStartPipelineExecutionOrderHelper(t *testing.T) {
	if os.Getenv("PKG_MASTER_START_ORDER_HELPER") != "1" {
		return
	}
	events := make([]string, 0, 3)
	mainPipelines = []Pipeline{
		&recordingPipeline{name: "crontab", events: &events},
		&recordingPipeline{name: "business", events: &events},
		&recordingPipeline{name: "addons", events: &events, startErr: errors.New("stop before scheduler"), startHook: func() {
			fmt.Println("PKG_MASTER_MAIN_ORDER=" + strings.Join(events, ","))
		}},
	}
	startPipeline()
}

// TestRegisterCommonFuncs 验证框架注册的默认远端配置函数可通过 common 调用表访问.
func TestRegisterCommonFuncs(t *testing.T) {
	preserveMasterPackageState(t)
	registerCommonFuncs()
	fn, ok := common.Funcs.Load("GetDataSource")
	assert.True(t, ok)
	err := fn("invalid")
	assert.True(t, err != nil)
	assert.Contains(t, "invalid data source configuration", err.Error())
}

// TestStartConfigPipelineFatal 使用独立测试进程冻结启动错误的现有退出语义.
func TestStartConfigPipelineFatal(t *testing.T) {
	output, err := runMasterSubprocess(t, "TestStartConfigPipelineFatalHelper", "PKG_MASTER_FATAL_HELPER")
	assert.True(t, err != nil)
	var exitErr *exec.ExitError
	assert.True(t, errors.As(err, &exitErr))
	assert.True(t, exitErr.ExitCode() != 0)
	assert.Contains(t, "Failed to initialize config: expected start failure", string(output))
}

// TestStartConfigPipelineFatalHelper 仅在父测试指定环境变量时触发 log.Fatal 分支.
func TestStartConfigPipelineFatalHelper(t *testing.T) {
	if os.Getenv("PKG_MASTER_FATAL_HELPER") != "1" {
		return
	}
	var events []string
	configPipelines = []Pipeline{&recordingPipeline{name: "failing", events: &events, startErr: errors.New("expected start failure")}}
	startConfigPipeline()
}

// TestStartPipelineFatal 使用独立测试进程冻结 MainStage 启动错误的现有退出语义.
func TestStartPipelineFatal(t *testing.T) {
	output, err := runMasterSubprocess(t, "TestStartPipelineFatalHelper", "PKG_MASTER_MAIN_FATAL_HELPER")
	assert.True(t, err != nil)
	var exitErr *exec.ExitError
	assert.True(t, errors.As(err, &exitErr))
	assert.True(t, exitErr.ExitCode() != 0)
	assert.Contains(t, "Failed to initialize main: expected start failure", string(output))
}

// TestStartPipelineFatalHelper 仅在父测试指定环境变量时触发 MainStage log.Fatal 分支.
func TestStartPipelineFatalHelper(t *testing.T) {
	if os.Getenv("PKG_MASTER_MAIN_FATAL_HELPER") != "1" {
		return
	}
	var events []string
	mainPipelines = []Pipeline{&recordingPipeline{name: "failing", events: &events, startErr: errors.New("expected start failure")}}
	startPipeline()
}

// TestStartRemotePipelinesRunsApplicationStages 验证禁用框架远端任务时仍按注册顺序执行应用 RemoteStage.
func TestStartRemotePipelinesRunsApplicationStages(t *testing.T) {
	prepareMasterConfig(t)
	ctx := context.WithValue(context.Background(), masterContextKey{}, "value")
	var events []string
	RegisterWithContext(
		RemoteStage,
		func(got context.Context) {
			assert.Equal(t, "value", got.Value(masterContextKey{}))
			events = append(events, "first")
		},
		func(context.Context) { events = append(events, "second") },
	)

	startRemotePipelines(ctx)
	assert.Equal(t, []string{"first", "second"}, events)
}

// TestGetRemoteConfStopsAfterCancellation 验证远端循环最终观察 context 取消并停止调用.
func TestGetRemoteConfStopsAfterCancellation(t *testing.T) {
	prepareMasterConfig(t)
	called := make(chan struct{}, 1)
	common.Funcs.Store("test-remote", func(any) error {
		called <- struct{}{}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := startRemoteConf(ctx, config.FilesConf{Method: "test-remote", Path: "local", RandomWait: 1, GetConfDuration: 2 * time.Millisecond})
	t.Cleanup(func() { cancel(); <-done })

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("remote config callback was not invoked")
	}
	cancel()
	select {
	case <-called:
		t.Fatal("remote config callback continued after cancellation window")
	case <-time.After(20 * time.Millisecond):
	}
}

// TestGetRemoteConfExitsDuringIntervalSleep 验证周期等待期间取消后不再调用配置方法.
// 热更新会立刻再起一组 fetcher, 旧协程若睡死仍会写同一配置文件.
func TestGetRemoteConfExitsDuringIntervalSleep(t *testing.T) {
	prepareMasterConfig(t)
	preserveRemoteWait(t)
	waitEntered := make(chan struct{})
	release := make(chan struct{}, 1)
	remoteRandomWaitSeconds = func(int) int { return 0 }
	remoteWait = gatedCancelableRemoteWait(waitEntered, release)

	var n atomic.Int32
	common.Funcs.Store("test-remote-interval", func(any) error {
		n.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := startRemoteConf(ctx, config.FilesConf{
		Method:          "test-remote-interval",
		Path:            "local",
		RandomWait:      0,
		GetConfDuration: time.Second,
	})
	t.Cleanup(func() { cancel(); <-done })

	// 放行首次随机等待 (0s) , 让配置方法先执行一次.
	waitForRemoteWait(t, waitEntered)
	releaseOneWait(release)
	deadline := time.Now().Add(time.Second)
	for n.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n.Load() == 0 {
		t.Fatal("remote config callback was not invoked before interval wait")
	}

	// 进入周期等待后取消, 再放行这次等待. 不可取消实现会继续下一轮拉取.
	waitForRemoteWait(t, waitEntered)
	cancel()
	releaseOneWait(release)
	if waitForRemoteWaitOK(waitEntered, 200*time.Millisecond) {
		t.Fatalf("remote config callback count = %d, want no extra wait after cancel during interval wait", n.Load())
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("remote config callback count = %d, want 1 after cancel during interval wait", got)
	}
}

// TestGetRemoteConfExitsDuringRandomWait 验证首次随机等待期间取消后不会调用配置方法.
func TestGetRemoteConfExitsDuringRandomWait(t *testing.T) {
	prepareMasterConfig(t)
	preserveRemoteWait(t)
	waitEntered := make(chan struct{})
	release := make(chan struct{}, 1)
	remoteRandomWaitSeconds = func(int) int { return 1 }
	remoteWait = gatedCancelableRemoteWait(waitEntered, release)

	var n atomic.Int32
	common.Funcs.Store("test-remote-wait", func(any) error {
		n.Add(1)
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := startRemoteConf(ctx, config.FilesConf{
		Method:          "test-remote-wait",
		Path:            "local",
		RandomWait:      2,
		GetConfDuration: time.Second,
	})
	t.Cleanup(func() { cancel(); <-done })
	waitForRemoteWait(t, waitEntered)
	cancel()
	releaseOneWait(release)
	if waitForRemoteWaitOK(waitEntered, 200*time.Millisecond) {
		t.Fatal("fetcher continued after cancel during random wait")
	}
	if got := n.Load(); got != 0 {
		t.Fatalf("remote config callback count = %d, want 0 after cancel during random wait", got)
	}
}

// preserveRemoteWait 保存远端等待函数并在测试结束时恢复.
func preserveRemoteWait(t *testing.T) {
	t.Helper()
	oldWait := remoteWait
	oldRandom := remoteRandomWaitSeconds
	t.Cleanup(func() {
		remoteWait = oldWait
		remoteRandomWaitSeconds = oldRandom
	})
}

// waitForRemoteWait 等待 fetcher 进入下一次 remoteWait.
func waitForRemoteWait(t *testing.T, waitEntered <-chan struct{}) {
	t.Helper()
	if !waitForRemoteWaitOK(waitEntered, time.Second) {
		t.Fatal("remote waiter was not entered")
	}
}

// waitForRemoteWaitOK 在超时前等待 fetcher 进入下一次 remoteWait.
func waitForRemoteWaitOK(waitEntered <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-waitEntered:
		return true
	case <-time.After(timeout):
		return false
	}
}

// releaseOneWait 放行一次被测试卡住的等待.
func releaseOneWait(release chan struct{}) {
	release <- struct{}{}
}

// gatedCancelableRemoteWait 按次卡住等待, 放行后若 ctx 已取消则结束循环.
func gatedCancelableRemoteWait(waitEntered chan struct{}, release <-chan struct{}) func(context.Context, time.Duration) bool {
	return func(ctx context.Context, _ time.Duration) bool {
		select {
		case waitEntered <- struct{}{}:
		case <-ctx.Done():
			return false
		}
		select {
		case <-release:
		case <-ctx.Done():
			return false
		}
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
}

// runMasterSubprocess 在限定时间内执行单个 helper 测试, 防止退出路径回归为永久阻塞.
func runMasterSubprocess(t *testing.T, testName, envName string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), envName+"=1")
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("master helper %s exceeded 5s: %s", testName, output)
	}
	return output, err
}

// masterContextKey 为测试 context value 提供不可冲突的键类型.
type masterContextKey struct{}
