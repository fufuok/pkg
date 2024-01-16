package master

import (
	"context"
	"slices"
	"sync"
)

const (
	// ConfigStage 程序启动和配置重载时优先执行
	ConfigStage Stage = iota

	// MainStage 在 ConfigStage 执行完成后执行
	MainStage

	// RemoteStage 执行定时远程获取配置
	RemoteStage
)

var (
	mu              sync.Mutex
	configPipelines []Pipeline
	mainPipelines   []Pipeline
	remotePipelines []ContextFunc
)

type Stage int
type ContextFunc func(ctx context.Context)
type Pipeline interface {
	Start() error
	Runtime() error
	Stop() error
}

func Register(stage Stage, sf ...Pipeline) {
	mu.Lock()
	defer mu.Unlock()
	switch stage {
	case ConfigStage:
		configPipelines = append(configPipelines, sf...)
	case MainStage:
		mainPipelines = append(mainPipelines, sf...)
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

func getPipelines(stage Stage) (ps []Pipeline) {
	switch stage {
	case ConfigStage:
		ps = configPipelines
	case MainStage:
		ps = mainPipelines
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
