package master

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/crontab"
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
	assert.True(t, reflect.TypeOf(configItems[0]) == reflect.TypeOf(&config.M{}))
	assert.True(t, reflect.TypeOf(configItems[1]) == reflect.TypeOf(&common.M{}))
	assert.True(t, configItems[2] == businessConfig)
	assert.True(t, reflect.TypeOf(mainItems[0]) == reflect.TypeOf(&crontab.M{}))
	assert.True(t, mainItems[1] == businessMain)
	assert.True(t, reflect.TypeOf(mainItems[2]) == reflect.TypeOf(&addons{}))
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

// TestPipelineRuntimeErrorsAreLogged 使用子进程初始化真实 logger, 验证两类 Runtime 错误均可观察.
func TestPipelineRuntimeErrorsAreLogged(t *testing.T) {
	output, err := runMasterSubprocess(t, "TestPipelineRuntimeErrorsAreLoggedHelper", "PKG_MASTER_RUNTIME_LOG_HELPER")
	assert.Nil(t, err)
	assert.Contains(t, "Runtime config pipeline failed", string(output))
	assert.Contains(t, "expected config runtime error", string(output))
	assert.Contains(t, "Runtime main pipeline failed", string(output))
	assert.Contains(t, "expected main runtime error", string(output))
}

// TestPipelineRuntimeErrorsAreLoggedHelper 在独立进程中建立控制台 logger, 避免污染父进程全局日志状态.
func TestPipelineRuntimeErrorsAreLoggedHelper(t *testing.T) {
	if os.Getenv("PKG_MASTER_RUNTIME_LOG_HELPER") != "1" {
		return
	}
	config.Debug = true
	config.InitTester()
	defer config.StopTester()
	if err := (&common.M{}).Runtime(); err != nil {
		t.Fatalf("initialize runtime logger: %v", err)
	}

	var events []string
	configPipelines = []Pipeline{&recordingPipeline{name: "config", events: &events, runtimeErr: errors.New("expected config runtime error")}}
	mainPipelines = []Pipeline{&recordingPipeline{name: "main", events: &events, runtimeErr: errors.New("expected main runtime error")}}
	runtimeConfigPipeline()
	runtimePipeline()
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
	RegisterWithContext(RemoteStage,
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
// 当前实现的 sleep 不可中断, 因此测试只要求在一个短周期后收敛, 不断言即时退出.
func TestGetRemoteConfStopsAfterCancellation(t *testing.T) {
	prepareMasterConfig(t)
	called := make(chan struct{})
	common.Funcs.Store("test-remote", func(any) error {
		called <- struct{}{}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	GetRemoteConf(ctx, config.FilesConf{Method: "test-remote", Path: "local", RandomWait: 1, GetConfDuration: 2 * time.Millisecond})

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
