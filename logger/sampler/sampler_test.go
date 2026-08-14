//nolint:zerologlint
package sampler

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/fufuok/ants"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
)

// samplerHook 为 Hook 门面增加可观察字段.
type samplerHook struct{}

// TestMain 在测试进程退出前等待 ants 默认池后台 goroutine 结束.
// logger 门面会导入 common 及其默认池, Linux race 退出阶段必须显式释放.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := ants.ReleaseTimeout(5 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "release ants default pool: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

// Run 在采样事件中写入固定字段.
func (samplerHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("hook", "sampler")
}

// TestSamplerFacadeContract 验证采样门面的级别、上下文和事件构造契约.
// Output 会主动覆盖输出目标, Fatal 和 Panic 不发送事件时也没有可观察副作用;
// 这三个薄包装这里只验证 zerolog 契约, 其目标选择由对应的一行生产实现保证.
func TestSamplerFacadeContract(t *testing.T) {
	buffer := installSampledLogger(t)

	var output bytes.Buffer
	outputLogger := Output(&output)
	outputLogger.Info().Msg("Output sample")
	assertSampleRecord(t, decodeSampleRecords(t, &output), "Output sample", zerolog.InfoLevel, nil)

	withLogger := With().Str("scope", "sampled").Logger()
	withLogger.Info().Msg("With sample")
	levelLogger := Level(zerolog.ErrorLevel)
	levelLogger.Info().Msg("Filtered sample")
	levelLogger.Error().Msg("Level sample")
	sampleLogger := Sample(&zerolog.BasicSampler{N: 1})
	sampleLogger.Info().Msg("Sample message")
	hookLogger := Hook(samplerHook{})
	hookLogger.Info().Msg("Hook sample")
	Err(errors.New("sample boom")).Msg("Error context")
	Err(nil).Msg("Nil error")
	Trace().Msg("Trace sample")
	Debug().Msg("Debug sample")
	Info().Msg("Info sample")
	Warn().Msg("Warn sample")
	Error().Msg("Error sample")
	WithLevel(zerolog.WarnLevel).Msg("WithLevel sample")
	Log().Msg("Log sample")
	Print("Print sample ", 1)
	Printf("Printf sample %d", 2)

	ctx := common.LogSampled().WithContext(context.Background())
	Ctx(ctx).Info().Msg("Context sample")
	if Ctx(context.Background()).GetLevel() != zerolog.Disabled {
		t.Fatal("context without logger did not return a disabled logger")
	}
	if Fatal() == nil || Panic() == nil {
		t.Fatal("fatal or panic sampler facade returned nil event")
	}

	records := decodeSampleRecords(t, buffer)
	if _, ok := records["Filtered sample"]; ok {
		t.Fatal("Level allowed a sampled event below the configured threshold")
	}
	assertSampleRecord(t, records, "With sample", zerolog.InfoLevel, map[string]any{"scope": "sampled"})
	assertSampleRecord(t, records, "Level sample", zerolog.ErrorLevel, nil)
	assertSampleRecord(t, records, "Sample message", zerolog.InfoLevel, nil)
	assertSampleRecord(t, records, "Hook sample", zerolog.InfoLevel, map[string]any{"hook": "sampler"})
	assertSampleRecord(t, records, "Error context", zerolog.ErrorLevel, map[string]any{zerolog.ErrorFieldName: "sample boom"})
	assertSampleRecord(t, records, "Nil error", zerolog.InfoLevel, nil)
	assertSampleRecord(t, records, "Trace sample", zerolog.TraceLevel, nil)
	assertSampleRecord(t, records, "Debug sample", zerolog.DebugLevel, nil)
	assertSampleRecord(t, records, "Info sample", zerolog.InfoLevel, nil)
	assertSampleRecord(t, records, "Warn sample", zerolog.WarnLevel, nil)
	assertSampleRecord(t, records, "Error sample", zerolog.ErrorLevel, nil)
	assertSampleRecord(t, records, "WithLevel sample", zerolog.WarnLevel, nil)
	assertSampleRecord(t, records, "Log sample", zerolog.NoLevel, nil)
	assertSampleRecord(t, records, "Print sample 1", zerolog.DebugLevel, nil)
	assertSampleRecord(t, records, "Printf sample 2", zerolog.DebugLevel, nil)
	assertSampleRecord(t, records, "Context sample", zerolog.InfoLevel, nil)
}

// installSampledLogger 临时替换 common 当前返回的采样 logger 值, 测试结束后恢复.
// common 未初始化时可能回退到普通 logger, 这里验证门面行为但不声明原子指针独立性.
func installSampledLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	loggerPointer := common.LogSampled()
	original := *loggerPointer
	buffer := &bytes.Buffer{}
	*loggerPointer = zerolog.New(buffer).Level(zerolog.TraceLevel)
	t.Cleanup(func() {
		*loggerPointer = original
	})
	return buffer
}

// decodeSampleRecords 将采样日志 JSON 行转换为按消息索引的记录.
func decodeSampleRecords(t *testing.T, buffer *bytes.Buffer) map[string]map[string]any {
	t.Helper()
	records := make(map[string]map[string]any)
	for line := range bytes.SplitSeq(bytes.TrimSpace(buffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := stdjson.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode sampled log line %q: %v", line, err)
		}
		message, _ := record[zerolog.MessageFieldName].(string)
		records[message] = record
	}
	return records
}

// assertSampleRecord 验证采样日志的级别和指定字段.
func assertSampleRecord(t *testing.T, records map[string]map[string]any, message string, level zerolog.Level, fields map[string]any) {
	t.Helper()
	record, ok := records[message]
	if !ok {
		t.Fatalf("sampled log message %q was not written", message)
	}
	if level == zerolog.NoLevel {
		if _, exists := record[zerolog.LevelFieldName]; exists {
			t.Fatalf("sampled log message %q unexpectedly has a level: %+v", message, record)
		}
	} else if got := record[zerolog.LevelFieldName]; got != level.String() {
		t.Fatalf("sampled log message %q level = %v, want %s", message, got, level)
	}
	for key, want := range fields {
		if got := record[key]; got != want {
			t.Fatalf("sampled log message %q field %q = %v, want %v", message, key, got, want)
		}
	}
}
