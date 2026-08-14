//nolint:zerologlint
package alarm

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

// alarmHook 为 Hook 门面增加可观察字段.
type alarmHook struct{}

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

// Run 在报警事件中写入固定字段.
func (alarmHook) Run(e *zerolog.Event, _ zerolog.Level, _ string) {
	e.Str("hook", "alarm")
}

// TestAlarmFacadeContract 验证报警门面的级别、上下文和事件构造契约.
// Output 会主动覆盖输出目标, Fatal 和 Panic 不发送事件时也没有可观察副作用;
// 这三个薄包装这里只验证 zerolog 契约, 其目标选择由对应的一行生产实现保证.
func TestAlarmFacadeContract(t *testing.T) {
	buffer := installAlarmLogger(t)

	var output bytes.Buffer
	outputLogger := Output(&output)
	outputLogger.Warn().Msg("Output alarm")
	assertAlarmRecord(t, decodeAlarmRecords(t, &output), "Output alarm", zerolog.WarnLevel, nil)

	withLogger := With().Str("scope", "alarm").Logger()
	withLogger.Warn().Msg("With alarm")
	levelLogger := Level(zerolog.ErrorLevel)
	levelLogger.Warn().Msg("Filtered alarm")
	levelLogger.Error().Msg("Level alarm")
	sampleLogger := Sample(&zerolog.BasicSampler{N: 1})
	sampleLogger.Warn().Msg("Sample alarm")
	hookLogger := Hook(alarmHook{})
	hookLogger.Warn().Msg("Hook alarm")
	Err(errors.New("alarm boom")).Msg("Error context")
	Err(nil).Msg("Nil error")
	Trace().Msg("Trace alarm")
	Debug().Msg("Debug alarm")
	Info().Msg("Info alarm")
	Warn().Msg("Warn alarm")
	Error().Msg("Error alarm")
	WithLevel(zerolog.ErrorLevel).Msg("WithLevel alarm")
	Log().Msg("Log alarm")
	Print("Print alarm ", 1)
	Printf("Printf alarm %d", 2)

	ctx := common.LogAlarm().WithContext(context.Background())
	Ctx(ctx).Warn().Msg("Context alarm")
	if Ctx(context.Background()).GetLevel() != zerolog.Disabled {
		t.Fatal("context without logger did not return a disabled logger")
	}
	if Fatal() == nil || Panic() == nil {
		t.Fatal("fatal or panic alarm facade returned nil event")
	}

	records := decodeAlarmRecords(t, buffer)
	if _, ok := records["Filtered alarm"]; ok {
		t.Fatal("Level allowed an alarm below the configured threshold")
	}
	assertAlarmRecord(t, records, "With alarm", zerolog.WarnLevel, map[string]any{"scope": "alarm"})
	assertAlarmRecord(t, records, "Level alarm", zerolog.ErrorLevel, nil)
	assertAlarmRecord(t, records, "Sample alarm", zerolog.WarnLevel, nil)
	assertAlarmRecord(t, records, "Hook alarm", zerolog.WarnLevel, map[string]any{"hook": "alarm"})
	assertAlarmRecord(t, records, "Error context", zerolog.ErrorLevel, map[string]any{zerolog.ErrorFieldName: "alarm boom"})
	assertAlarmRecord(t, records, "Nil error", zerolog.InfoLevel, nil)
	assertAlarmRecord(t, records, "Trace alarm", zerolog.TraceLevel, nil)
	assertAlarmRecord(t, records, "Debug alarm", zerolog.DebugLevel, nil)
	assertAlarmRecord(t, records, "Info alarm", zerolog.InfoLevel, nil)
	assertAlarmRecord(t, records, "Warn alarm", zerolog.WarnLevel, nil)
	assertAlarmRecord(t, records, "Error alarm", zerolog.ErrorLevel, nil)
	assertAlarmRecord(t, records, "WithLevel alarm", zerolog.ErrorLevel, nil)
	assertAlarmRecord(t, records, "Log alarm", zerolog.NoLevel, nil)
	assertAlarmRecord(t, records, "Print alarm 1", zerolog.DebugLevel, nil)
	assertAlarmRecord(t, records, "Printf alarm 2", zerolog.DebugLevel, nil)
	assertAlarmRecord(t, records, "Context alarm", zerolog.WarnLevel, nil)
}

// installAlarmLogger 临时替换 common 当前返回的报警 logger 值, 测试结束后恢复.
// common 未初始化时可能回退到普通 logger, 这里验证门面行为但不声明原子指针独立性.
func installAlarmLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	loggerPointer := common.LogAlarm()
	original := *loggerPointer
	buffer := &bytes.Buffer{}
	*loggerPointer = zerolog.New(buffer).Level(zerolog.TraceLevel)
	t.Cleanup(func() {
		*loggerPointer = original
	})
	return buffer
}

// decodeAlarmRecords 将报警 JSON 行转换为按消息索引的记录.
func decodeAlarmRecords(t *testing.T, buffer *bytes.Buffer) map[string]map[string]any {
	t.Helper()
	records := make(map[string]map[string]any)
	for line := range bytes.SplitSeq(bytes.TrimSpace(buffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record map[string]any
		if err := stdjson.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode alarm line %q: %v", line, err)
		}
		message, _ := record[zerolog.MessageFieldName].(string)
		records[message] = record
	}
	return records
}

// assertAlarmRecord 验证报警记录的级别和指定字段.
func assertAlarmRecord(t *testing.T, records map[string]map[string]any, message string, level zerolog.Level, fields map[string]any) {
	t.Helper()
	record, ok := records[message]
	if !ok {
		t.Fatalf("alarm message %q was not written", message)
	}
	if level == zerolog.NoLevel {
		if _, exists := record[zerolog.LevelFieldName]; exists {
			t.Fatalf("alarm message %q unexpectedly has a level: %+v", message, record)
		}
	} else if got := record[zerolog.LevelFieldName]; got != level.String() {
		t.Fatalf("alarm message %q level = %v, want %s", message, got, level)
	}
	for key, want := range fields {
		if got := record[key]; got != want {
			t.Fatalf("alarm message %q field %q = %v, want %v", message, key, got, want)
		}
	}
}
