//nolint:zerologlint
package logger

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
)

// facadeHook 为 Hook 门面测试增加可观察字段, 避免只断言返回值非 nil.
type facadeHook struct{}

// Run 在事件写出前增加固定字段.
func (facadeHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("hook", "called")
}

// TestLoggerFacadeContract 验证普通日志门面的级别、上下文和事件构造契约.
// Output 会主动覆盖输出目标, Fatal 和 Panic 不发送事件时也没有可观察副作用;
// 这三个薄包装这里只验证 zerolog 契约, 其目标选择由对应的一行生产实现保证.
func TestLoggerFacadeContract(t *testing.T) {
	buffer := installFacadeLogger(t, common.Log)

	var output bytes.Buffer
	outputLogger := Output(&output)
	outputLogger.Info().Msg("Output message")
	assertLogRecord(t, &output, "Output message", zerolog.InfoLevel, nil)

	withLogger := With().Str("scope", "child").Logger()
	withLogger.Info().Msg("With message")
	levelLogger := Level(zerolog.ErrorLevel)
	levelLogger.Info().Msg("Filtered message")
	levelLogger.Error().Msg("Level message")
	sampleLogger := Sample(&zerolog.BasicSampler{N: 1})
	sampleLogger.Info().Msg("Sample message")
	hookLogger := Hook(facadeHook{})
	hookLogger.Info().Msg("Hook message")
	Err(errors.New("boom")).Msg("Error context")
	Err(nil).Msg("Nil error")
	Trace().Msg("Trace message")
	Debug().Msg("Debug message")
	Info().Msg("Info message")
	Warn().Msg("Warn message")
	Error().Msg("Error message")
	WithLevel(zerolog.WarnLevel).Msg("WithLevel message")
	Log().Msg("Log message")
	Print("Print ", 1)
	Printf("Printf %d", 2)

	ctx := common.Log().WithContext(context.Background())
	Ctx(ctx).Info().Msg("Context message")
	if Ctx(context.Background()).GetLevel() != zerolog.Disabled {
		t.Fatal("context without logger did not return a disabled logger")
	}

	// Fatal 和 Panic 只有在 Msg 时才终止或 panic; 这里只验证事件可构造且不改变进程状态,
	// 不声明 fallback logger 场景下无法区分的目标路由.
	if Fatal() == nil || Panic() == nil {
		t.Fatal("fatal or panic facade returned nil event")
	}

	records := decodeLogRecords(t, buffer)
	if _, ok := records["Filtered message"]; ok {
		t.Fatal("Level allowed an info event below the configured threshold")
	}
	assertRecord(t, records, "With message", zerolog.InfoLevel, map[string]any{"scope": "child"})
	assertRecord(t, records, "Level message", zerolog.ErrorLevel, nil)
	assertRecord(t, records, "Sample message", zerolog.InfoLevel, nil)
	assertRecord(t, records, "Hook message", zerolog.InfoLevel, map[string]any{"hook": "called"})
	assertRecord(t, records, "Error context", zerolog.ErrorLevel, map[string]any{zerolog.ErrorFieldName: "boom"})
	assertRecord(t, records, "Nil error", zerolog.InfoLevel, nil)
	assertRecord(t, records, "Trace message", zerolog.TraceLevel, nil)
	assertRecord(t, records, "Debug message", zerolog.DebugLevel, nil)
	assertRecord(t, records, "Info message", zerolog.InfoLevel, nil)
	assertRecord(t, records, "Warn message", zerolog.WarnLevel, nil)
	assertRecord(t, records, "Error message", zerolog.ErrorLevel, nil)
	assertRecord(t, records, "WithLevel message", zerolog.WarnLevel, nil)
	assertRecord(t, records, "Log message", zerolog.NoLevel, nil)
	assertRecord(t, records, "Print 1", zerolog.DebugLevel, nil)
	assertRecord(t, records, "Printf 2", zerolog.DebugLevel, nil)
	assertRecord(t, records, "Context message", zerolog.InfoLevel, nil)
}

// installFacadeLogger 临时替换 common 当前返回的 logger 值, 测试结束后恢复.
// common 未初始化时返回包级 fallback logger, 直接替换其值可避免触发 Runtime
// 改写原子指针和日志配置. 测试会修改共享值, 不得并行执行.
func installFacadeLogger(t *testing.T, target func() *zerolog.Logger) *bytes.Buffer {
	t.Helper()
	loggerPointer := target()
	original := *loggerPointer
	buffer := &bytes.Buffer{}
	replacement := zerolog.New(buffer).Level(zerolog.TraceLevel)
	*loggerPointer = replacement

	t.Cleanup(func() {
		*loggerPointer = original
	})
	return buffer
}

// decodeLogRecords 将每行 zerolog JSON 转换为按消息索引的记录.
func decodeLogRecords(t *testing.T, buffer *bytes.Buffer) map[string]map[string]any {
	t.Helper()
	records := make(map[string]map[string]any)
	for line := range bytes.SplitSeq(bytes.TrimSpace(buffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := stdjson.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		message, _ := record[zerolog.MessageFieldName].(string)
		records[message] = record
	}
	return records
}

// assertLogRecord 验证只包含一条记录的 writer 输出.
func assertLogRecord(t *testing.T, buffer *bytes.Buffer, message string, level zerolog.Level, fields map[string]any) {
	t.Helper()
	assertRecord(t, decodeLogRecords(t, buffer), message, level, fields)
}

// assertRecord 验证消息的级别和指定结构化字段.
func assertRecord(t *testing.T, records map[string]map[string]any, message string, level zerolog.Level, fields map[string]any) {
	t.Helper()
	record, ok := records[message]
	if !ok {
		t.Fatalf("log message %q was not written", message)
	}
	if level == zerolog.NoLevel {
		if _, exists := record[zerolog.LevelFieldName]; exists {
			t.Fatalf("log message %q unexpectedly has a level: %+v", message, record)
		}
	} else if got := record[zerolog.LevelFieldName]; got != level.String() {
		t.Fatalf("log message %q level = %v, want %s", message, got, level)
	}
	for key, want := range fields {
		if got := record[key]; got != want {
			t.Fatalf("log message %q field %q = %v, want %v", message, key, got, want)
		}
	}
}
