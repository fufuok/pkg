package logger

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fufuok/ants"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

var (
	customLoggerFixtureOnce sync.Once
	customLoggerFixture     *CustomLogger
	customLoggerFixturePath string
	customLoggerFixtureDir  string
	customLoggerFixtureErr  error
)

// trackingCloser 记录 CustomLogger 是否转发 Close, 并提供确定性错误结果.
type trackingCloser struct {
	closed bool
	err    error
}

// TestMain 在全部 logger 测试结束后释放文件 logger、ants 默认池和临时目录.
// Linux race 运行器退出前必须等待默认池后台 goroutine; 文件 logger 在同一
// 测试进程只构造一次, 避免 -count 重复门禁累积 roller 资源.
func TestMain(m *testing.M) {
	code := m.Run()
	if customLoggerFixture != nil {
		if err := customLoggerFixture.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close custom logger fixture: %v\n", err)
			code = 1
		}
	}
	if err := ants.ReleaseTimeout(5 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "release ants default pool: %v\n", err)
		code = 1
	}
	if customLoggerFixtureDir != "" {
		if err := os.RemoveAll(customLoggerFixtureDir); err != nil {
			fmt.Fprintf(os.Stderr, "remove custom logger fixture directory: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

// Close 标记资源已关闭并返回预设错误.
func (c *trackingCloser) Close() error {
	c.closed = true
	return c.err
}

// TestCustomLoggerOptions 验证所有函数选项只修改对应字段.
func TestCustomLoggerOptions(t *testing.T) {
	options := &CustomLoggerOptions{}
	WithLogFile("app.log")(options)
	WithLogLevel(zerolog.WarnLevel)(options)
	WithMaxSize(8)(options)
	WithMaxAge(9)(options)
	WithMaxBackups(10)(options)

	if options.logFile != "app.log" || options.level != zerolog.WarnLevel ||
		options.maxSize != 8 || options.maxAge != 9 || options.maxBackups != 10 {
		t.Fatalf("custom logger options = %+v", options)
	}
}

// TestCustomLoggerClose 验证无 closer 和有 closer 两类资源关闭语义.
func TestCustomLoggerClose(t *testing.T) {
	if err := (&CustomLogger{}).Close(); err != nil {
		t.Fatalf("close logger without resource: %v", err)
	}

	wantErr := errors.New("close failed")
	closer := &trackingCloser{err: wantErr}
	if err := (&CustomLogger{closer: closer}).Close(); !errors.Is(err, wantErr) {
		t.Fatalf("close logger error = %v, want %v", err, wantErr)
	}
	if !closer.closed {
		t.Fatal("custom logger did not close its resource")
	}
}

// TestNewCustomFileLoggerValidation 验证文件路径和日志级别在创建资源前被拒绝.
func TestNewCustomFileLoggerValidation(t *testing.T) {
	config.InitTester()
	t.Cleanup(config.StopTester)

	if _, err := NewCustomFileLogger(WithLogFile("")); err == nil || err.Error() != "log file path cannot be empty" {
		t.Fatalf("empty log path error = %v", err)
	}
	if _, err := NewCustomFileLogger(WithLogFile(filepath.Join(t.TempDir(), "invalid.log")), WithLogLevel(zerolog.NoLevel)); err == nil || !strings.HasPrefix(err.Error(), "invalid log level:") {
		t.Fatalf("invalid log level error = %v", err)
	}
}

// TestNewCustomFileLoggerWrites 验证边界选项下的真实文件 writer 和 JSON 输出.
// 包内构造内核返回显式 Close 所有权, TestMain 在所有 -count 迭代结束后统一释放.
func TestNewCustomFileLoggerWrites(t *testing.T) {
	config.InitTester()
	t.Cleanup(config.StopTester)
	config.Config().LogConf.NoPretty = true

	fixture, logFile, err := getCustomLoggerFixture()
	if err != nil {
		t.Fatal(err)
	}
	fixture.Logger.Info().Str("source", "fixture").Msg("Custom logger works")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read custom log file: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(content), []byte("\n"))
	var record map[string]any
	if err := stdjson.Unmarshal(lines[len(lines)-1], &record); err != nil {
		t.Fatalf("decode custom log %q: %v", lines[len(lines)-1], err)
	}
	if record[zerolog.MessageFieldName] != "Custom logger works" || record["source"] != "fixture" {
		t.Fatalf("custom log record = %+v", record)
	}
	if record[zerolog.LevelFieldName] != zerolog.InfoLevel.String() {
		t.Fatalf("custom log level = %v, want info", record[zerolog.LevelFieldName])
	}
}

// getCustomLoggerFixture 按测试进程构造一次真实 roller, 并返回稳定的日志路径.
// 调用方必须已初始化测试配置; 资源由 TestMain 统一关闭和删除.
func getCustomLoggerFixture() (*CustomLogger, string, error) {
	customLoggerFixtureOnce.Do(func() {
		customLoggerFixtureDir, customLoggerFixtureErr = os.MkdirTemp("", "pkg-customlogger-fixture-")
		if customLoggerFixtureErr != nil {
			customLoggerFixtureErr = fmt.Errorf("create custom logger fixture directory: %w", customLoggerFixtureErr)
			return
		}

		customLoggerFixturePath = filepath.Join(customLoggerFixtureDir, "custom.log")
		customLoggerFixture, customLoggerFixtureErr = newCustomFileLogger(
			WithLogFile(customLoggerFixturePath),
			WithLogLevel(zerolog.TraceLevel),
			WithMaxSize(0),
			WithMaxAge(0),
			WithMaxBackups(-1),
		)
		if customLoggerFixtureErr != nil {
			customLoggerFixtureErr = fmt.Errorf("create custom logger fixture: %w", customLoggerFixtureErr)
		}
	})
	return customLoggerFixture, customLoggerFixturePath, customLoggerFixtureErr
}
