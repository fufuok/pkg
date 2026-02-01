package logger

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/natefinch/lumberjack/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
)

// CustomLogger 自定义日志记录器（内部使用）
type CustomLogger struct {
	Logger *zerolog.Logger
	closer io.Closer
}

// Close 关闭日志记录器
func (cl *CustomLogger) Close() error {
	if cl.closer != nil {
		return cl.closer.Close()
	}
	return nil
}

// CustomLoggerOptions 自定义日志记录器选项
type CustomLoggerOptions struct {
	logFile    string
	level      zerolog.Level
	maxSize    int64
	maxAge     int
	maxBackups int
}

// CustomLoggerOption 自定义日志记录器选项函数类型
type CustomLoggerOption func(*CustomLoggerOptions)

// WithLogFile 设置日志文件路径
func WithLogFile(logFile string) CustomLoggerOption {
	return func(o *CustomLoggerOptions) {
		o.logFile = logFile
	}
}

// WithLogLevel 设置日志级别
func WithLogLevel(level zerolog.Level) CustomLoggerOption {
	return func(o *CustomLoggerOptions) {
		o.level = level
	}
}

// WithMaxSize 设置日志文件最大大小 (MB)
func WithMaxSize(maxSize int64) CustomLoggerOption {
	return func(o *CustomLoggerOptions) {
		o.maxSize = maxSize
	}
}

// WithMaxAge 设置日志文件最大保留时间 (天)
func WithMaxAge(maxAge int) CustomLoggerOption {
	return func(o *CustomLoggerOptions) {
		o.maxAge = maxAge
	}
}

// WithMaxBackups 设置日志文件最大备份数
func WithMaxBackups(maxBackups int) CustomLoggerOption {
	return func(o *CustomLoggerOptions) {
		o.maxBackups = maxBackups
	}
}

// NewCustomFileLogger 创建自定义文件日志记录器
// 返回一个 *zerolog.Logger 对象, 系统会自动处理关闭操作
// 将日志写入指定文件并享受全局的日志切割和格式化配置
func NewCustomFileLogger(opts ...CustomLoggerOption) (*zerolog.Logger, error) {
	// 初始化默认选项
	defaultCfg := config.Config().LogConf
	options := &CustomLoggerOptions{
		logFile:    defaultCfg.File,                 // 使用全局配置的默认日志文件
		level:      zerolog.Level(defaultCfg.Level), // 使用全局配置的默认日志级别
		maxSize:    defaultCfg.MaxSize,              // 使用全局配置的默认文件大小
		maxAge:     defaultCfg.MaxAge,               // 使用全局配置的默认保留时间
		maxBackups: defaultCfg.MaxBackups,           // 使用全局配置的默认备份数
	}

	// 应用用户提供的选项
	for _, opt := range opts {
		opt(options)
	}

	// 参数验证
	if options.logFile == "" {
		return nil, errors.New("log file path cannot be empty")
	}

	if options.level < zerolog.TraceLevel || options.level > zerolog.PanicLevel {
		return nil, fmt.Errorf("invalid log level: %v", options.level)
	}

	// 确保配置值有效
	if options.maxSize <= 0 {
		options.maxSize = defaultCfg.MaxSize
	}

	if options.maxAge <= 0 {
		options.maxAge = defaultCfg.MaxAge
	}

	if options.maxBackups < 0 {
		options.maxBackups = defaultCfg.MaxBackups
	}

	// 创建文件滚动写入器
	fileWriter, err := lumberjack.NewRoller(
		options.logFile,
		options.maxSize*common.Megabyte,
		&lumberjack.Options{
			MaxAge:     time.Duration(options.maxAge) * common.Days,
			MaxBackups: options.maxBackups,
			LocalTime:  true,
			Compress:   true,
		})
	if err != nil {
		return nil, fmt.Errorf("failed to create lumberjack roller: %w", err)
	}

	// 应用全局的格式化配置
	var wr io.Writer
	if !defaultCfg.NoPretty {
		wr = zerolog.ConsoleWriter{Out: fileWriter, NoColor: defaultCfg.NoColor, TimeFormat: common.LogTimeFormat}
	} else {
		wr = fileWriter
	}

	// 创建日志记录器
	l := zerolog.New(wr).With().Timestamp().Caller().Logger().Level(options.level)
	logPtr := &l

	// 创建内部 CustomLogger 对象用于管理资源
	customLogger := &CustomLogger{
		Logger: logPtr,
		closer: fileWriter,
	}

	// 设置终结器, 自动关闭资源
	runtime.SetFinalizer(logPtr, func(l *zerolog.Logger) {
		_ = customLogger.Close()
	})

	return logPtr, nil
}
