package common

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// lockedBuffer 为异步日志测试提供并发安全的内存输出.
//
// zerolog 与 ants 任务可能并发写入, 普通 bytes.Buffer 不满足该使用边界.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write 串行写入日志数据, 满足 io.Writer 契约.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// String 返回当前日志快照, 避免读取期间与异步写入发生竞争.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// prepareCommonConfig 隔离 common 状态并建立最小离线配置.
//
// 返回的配置只在当前测试生命周期内有效. 调用方不得并行执行依赖该助手的用例.
func prepareCommonConfig(t *testing.T) *config.MainConf {
	t.Helper()
	preserveCommonPackageState(t)
	oldDebug := config.Debug
	config.InitTester()
	t.Cleanup(func() {
		config.StopTester()
		config.Debug = oldDebug
	})
	return config.Config()
}

// prepareCommonRuntime 在离线配置基础上安装测试助手拥有的 ants pool.
//
// 该环境适用于异步报警和日志发送测试, cleanup 会按 pool 所有权协议恢复调用方资源.
func prepareCommonRuntime(t *testing.T) *config.MainConf {
	t.Helper()
	cfg := prepareCommonConfig(t)
	InitTester()
	t.Cleanup(StopTester)
	return cfg
}

// installCommonTestLoggers 把三类 logger 指向同一受控 writer, 便于验证可观察日志.
func installCommonTestLoggers(w io.Writer, level zerolog.Level) {
	base := zerolog.New(w).Level(level)
	sampled := base.With().Bool("sampling", true).Logger()
	alarm := base.With().Bool("alarm", true).Logger()
	logger.Store(&base)
	logSampled.Store(&sampled)
	logAlarm.Store(&alarm)
}

// waitCommonCondition 在限定时间内轮询异步可观察状态, 超时即报告具体条件.
func waitCommonCondition(t *testing.T, timeout time.Duration, name string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", name)
}
