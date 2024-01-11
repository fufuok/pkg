package master

import (
	"context"
	"slices"
	"sync"
)

const (
	// InitStage 程序初始化, 错误时退出程序
	InitStage Stage = iota

	// RuntimeStage 配置变化重载后运行
	RuntimeStage

	// StopStage 退出程序时执行
	StopStage

	// RemoteStage 执行定时远程获取配置
	RemoteStage
)

var (
	mu               sync.Mutex
	initPipelines    []StageFunc
	runtimePipelines []StageFunc
	stopPipelines    []StageFunc
	remotePipelines  []ContextFunc
)

type Stage int
type StageFunc func() error
type ContextFunc func(ctx context.Context)

func Register(stage Stage, sf ...StageFunc) {
	mu.Lock()
	defer mu.Unlock()
	switch stage {
	case InitStage:
		initPipelines = append(initPipelines, sf...)
	case RuntimeStage:
		runtimePipelines = append(runtimePipelines, sf...)
	case StopStage:
		stopPipelines = append(stopPipelines, sf...)
	}
}

func RegisterWithContext(stage Stage, sf ...ContextFunc) {
	mu.Lock()
	defer mu.Unlock()
	switch stage {
	case RemoteStage:
		remotePipelines = append(remotePipelines, sf...)
	}
}

// 运行 Pipelines, 顺序执行 (注意不要有阻塞)
func runPipelines(stage Stage) error {
	ps := getPipelines(stage)
	for _, sf := range ps {
		if err := sf(); err != nil {
			return err
		}
	}
	return nil
}

func getPipelines(stage Stage) (ps []StageFunc) {
	switch stage {
	case InitStage:
		ps = initPipelines
	case RuntimeStage:
		ps = runtimePipelines
	case StopStage:
		ps = stopPipelines
	}

	mu.Lock()
	ps = slices.Clone(ps)
	mu.Unlock()
	return
}

func getPipelinesWithContext(stage Stage) (ps []ContextFunc) {
	switch stage {
	case RemoteStage:
		ps = remotePipelines
	}

	mu.Lock()
	ps = slices.Clone(ps)
	mu.Unlock()
	return
}
