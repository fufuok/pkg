package common

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/xjson/gjson"
)

// TestLoggerInitializationAndRuntime 验证 nop 回退、字段名、级别、caller、采样器和 Runtime 可观察状态.
func TestLoggerInitializationAndRuntime(t *testing.T) {
	cfg := prepareCommonConfig(t)
	if Log() != &disabledLogger || LogSampled() != &disabledLogger || LogAlarm() != &disabledLogger {
		t.Fatal("uninitialized loggers did not return the nop logger")
	}

	config.Debug = true
	cfg.LogConf.Level = int(zerolog.InfoLevel)
	cfg.LogConf.NoColor = true
	cfg.LogConf.Burst = 1
	cfg.LogConf.PeriodDuration = time.Hour

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create logger pipe: %v", err)
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

	initLogger()
	newReq()
	Log().Debug().Msg("hidden debug")
	Log().Info().Str("case", "logger-init").Msg("visible info")
	LogSampled().Info().Msg("visible sampled")
	LogSampled().Info().Msg("hidden sampled")
	if Log() == &disabledLogger || LogSampled() == Log() || LogAlarm() == Log() {
		t.Fatal("initialized loggers did not publish distinct runtime instances")
	}
	if AppLoggerUseSampler {
		t.Fatal("debug mode unexpectedly enabled app logger sampling")
	}

	cfg.LogConf.Level = int(zerolog.ErrorLevel)
	cfg.SYSConf.ReqDebug = true
	if err := (&M{}).Runtime(); err != nil {
		t.Fatalf("runtime reload: %v", err)
	}
	Log().Warn().Msg("hidden warning")
	Log().Error().Msg("visible error")
	if !reqDebug || logCurrentConf != cfg.LogConf {
		t.Fatal("runtime reload did not publish logger and request state")
	}

	os.Stdout = oldStdout
	if err := writer.Close(); err != nil {
		t.Fatalf("close logger pipe: %v", err)
	}
	writerClosed = true
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read logger output: %v", err)
	}
	output := string(body)
	for _, want := range []string{"visible info", "logger-init", "visible sampled", "sampling=true", "visible error", "common.TestLoggerInitializationAndRuntime"} {
		if !strings.Contains(output, want) {
			t.Fatalf("logger output missing %q: %s", want, output)
		}
	}
	for _, hidden := range []string{"hidden debug", "hidden warning", "hidden sampled"} {
		if strings.Contains(output, hidden) {
			t.Fatalf("logger output contains filtered message %q", hidden)
		}
	}
	if zerolog.MessageFieldName != LogMessageFieldName || zerolog.ErrorFieldName != LogErrorFieldName || zerolog.CallerFieldName != "F" {
		t.Fatal("zerolog field names were not normalized")
	}
}

// TestLoggerAdaptersAndRecovery 验证应用、cron、Redis、writer 和 recovery 适配器输出错误上下文.
func TestLoggerAdaptersAndRecovery(t *testing.T) {
	cfg := prepareCommonConfig(t)
	cfg.LogConf.PostAlarmAPI = ""
	cfg.LogConf.AlarmCode = ""
	var output lockedBuffer
	installCommonTestLoggers(&output, zerolog.TraceLevel)
	AppLoggerUseSampler = true

	NewAppLogger().Debugf("app debug %d", 1)
	app := NewAppLogger(false)
	app.Infof("app info")
	app.Warnf("app warn")
	app.Printf("app print")
	app.Errorf("app error")
	cron := NewCronLogger(false)
	cron.Info("cron info", "job", 1)
	cron.Error(errors.New("cron failure"), "cron error", "job", 2)
	NewRedisLogger(false).Printf(context.Background(), "redis failure %d", 3)
	if n, err := NewAppLoggerWriter(false).Write([]byte("writer log")); err != nil || n != len("writer log") {
		t.Fatalf("app logger writer = %d, %v", n, err)
	}
	RecoverLogger("panic value", []byte("trace value"))
	RecoverAlarm(errors.New("alarm panic"), []byte("alarm trace"))

	for _, want := range []string{
		"app debug 1", "app info", "app warn", "app print", "app error",
		"cron info", "cron failure", "redis failure 3", "writer log",
		"panic value", "trace value", "alarm panic", "Sending alarm",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("adapter output missing %q: %s", want, output.String())
		}
	}
}

// TestAlarmDataAndDeliveryContract 验证报警字段、开关、级别过滤和本地 HTTP 投递.
func TestAlarmDataAndDeliveryContract(t *testing.T) {
	cfg := prepareCommonRuntime(t)
	cfg.NodeConf.NodeInfo = config.NodeInfo{
		Hostname: "host-a",
		HostIP:   "192.0.2.1",
		NodeID:   7,
		NodeIP:   "198.51.100.7",
		NodeName: "node-a",
		NodeDesc: "test node",
	}
	cfg.LogConf.AlarmCode = "DEFAULT"
	ErrMsgMaxLength = 8

	data := GenAlarmData("fallback", []byte(`{"M":"failed","E":"very long error","alarm_code":"OVERRIDE","more":"detail"}`))
	if got := gjson.GetBytes(data, "code").String(); got != "OVERRIDE" {
		t.Fatalf("alarm code = %q", got)
	}
	if got := gjson.GetBytes(data, "hostname").String(); got != "host-a" {
		t.Fatalf("alarm hostname = %q", got)
	}
	if info := gjson.GetBytes(data, "info").String(); !strings.Contains(info, "failed: ") || len(info) >= len("failed: very long error") {
		t.Fatalf("alarm info was not truncated: %q", info)
	}
	internal := genAlarmJson("DEFAULT", []byte(`{"M":"job failed","E":"boom","job":"sync"}`))
	if gjson.GetBytes(internal, "node_id").Int() != 7 || gjson.GetBytes(internal, "job").String() != "sync" {
		t.Fatalf("internal alarm fields = %s", internal)
	}

	bodies := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg.LogConf.PostAlarmAPI = server.URL
	req.SetCommonRetryCount(0)
	var output lockedBuffer
	installCommonTestLoggers(&output, zerolog.TraceLevel)

	SendAlarm("", "direct alarm", "more")
	assertCommonHTTPBody(t, bodies, "direct alarm")
	sendAlarm(func(string, []byte) []byte { return []byte(`{"kind":"sync"}`) }, []byte(`{}`))
	assertCommonHTTPBody(t, bodies, `"kind":"sync"`)

	logAlarmWriter = newAlarmWriter(zerolog.WarnLevel)
	logAlarmOnConf = true
	SetAlarmLevel(zerolog.WarnLevel)
	SetAlarmFunc(func(string, []byte) []byte { return []byte(`{"kind":"writer"}`) })
	SetAlarmOn()
	_, _ = logAlarmWriter.WriteLevel(zerolog.InfoLevel, []byte(`{"M":"ignored"}`))
	select {
	case body := <-bodies:
		t.Fatalf("below-level alarm was posted: %s", body)
	case <-time.After(20 * time.Millisecond):
	}
	_, _ = logAlarmWriter.WriteLevel(zerolog.ErrorLevel, []byte(`{"M":"writer alarm"}`))
	assertCommonHTTPBody(t, bodies, `"kind":"writer"`)

	SetAlarmOff()
	_, _ = logAlarmWriter.WriteLevel(zerolog.ErrorLevel, []byte(`{"M":"disabled"}`))
	select {
	case body := <-bodies:
		t.Fatalf("disabled alarm was posted: %s", body)
	case <-time.After(20 * time.Millisecond):
	}
}

// TestCommonHelpersAndConfigInvocation 验证 IP 段查询与远端配置函数分发的成功和失败边界.
func TestCommonHelpersAndConfigInvocation(t *testing.T) {
	prepareCommonConfig(t)
	_, ipNet, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatalf("parse CIDR: %v", err)
	}
	nets := map[*net.IPNet]int64{ipNet: 9}
	if got, ok := LookupIPNetsString("192.0.2.10", nets); !ok || got != 9 {
		t.Fatalf("string IP lookup = %d, %v", got, ok)
	}
	if _, ok := LookupIPNetsString("invalid", nets); ok {
		t.Fatal("invalid IP unexpectedly matched")
	}
	if got, ok := LookupIPNets(net.ParseIP("198.51.100.1"), nets); ok || got != 0 {
		t.Fatalf("outside IP lookup = %d, %v", got, ok)
	}

	conf := config.FilesConf{Method: "missing", Path: "local"}
	if err := InvokeConfigMethod(conf); !errors.Is(err, ErrInvalidGetter) {
		t.Fatalf("missing config method error = %v", err)
	}
	called := false
	Funcs.Store("fixture", func(args any) error {
		got, ok := args.(config.DataSourceArgs)
		if !ok || got.Conf.Path != "local" || got.Time.IsZero() {
			t.Fatalf("config method args = %#v", args)
		}
		called = true
		return nil
	})
	conf.Method = "fixture"
	if err := InvokeConfigMethod(conf); err != nil || !called {
		t.Fatalf("invoke config method = %v, called=%v", err, called)
	}
}

// assertCommonHTTPBody 等待本地 HTTP fixture 请求并断言稳定内容片段.
func assertCommonHTTPBody(t *testing.T, bodies <-chan string, want string) {
	t.Helper()
	select {
	case body := <-bodies:
		if !strings.Contains(body, want) {
			t.Fatalf("HTTP body missing %q: %s", want, body)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for HTTP body containing %q", want)
	}
}
