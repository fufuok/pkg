package common

import (
	"context"
	"io"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/utils"
	"github.com/natefinch/lumberjack/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
)

const (
	LogMessageFieldName = "M"
	LogErrorFieldName   = "E"
	LogTimeFormat       = "0102 15:04:05"

	// 文件滚动单位
	megabyte = 1024 * 1024
	days     = 24 * time.Hour
)

var (
	// AppLoggerUseSampler 初始化通用的 Req / Ants 等日志采样记录器
	// Debug 模式下默认为 false
	AppLoggerUseSampler = true

	// logger 通用日志, Debug 时输出到控制台, 否则写入日志文件
	logger atomic.Pointer[zerolog.Logger]

	// logSampled 抽样日志
	logSampled atomic.Pointer[zerolog.Logger]

	// logAlarm 报警日志, 写入通用日志并发送报警
	logAlarm atomic.Pointer[zerolog.Logger]

	logAlarmWriter = newAlarmWriter(zerolog.WarnLevel)
	logCurrentConf config.LogConf
	logAlarmOnConf bool
)

// Log 返回通用日志记录器
// common.Log().Debug().Msg("debug")
func Log() *zerolog.Logger {
	return logger.Load()
}

// LogSampled 返回抽样日志记录器
// common.LogSampled().Debug().Msg("debug")
func LogSampled() *zerolog.Logger {
	return logSampled.Load()
}

// LogAlarm 返回报警日志记录器
// common.LogAlarm().Warn().Err(err).Msg("alarm")
func LogAlarm() *zerolog.Logger {
	return logAlarm.Load()
}

func initLogger() {
	initZerolog()
	logAlarmOnConf = config.AlarmOn.Load()
	AppLoggerUseSampler = !config.Debug
	if err := loadLogger(); err != nil {
		log.Fatalln("Failed to initialize logger:", err, "\nbye.")
	}
}

//nolint:reassign
func initZerolog() {
	// 路径脱敏, 日志格式规范, 避免与自定义字段名冲突: {"E":"is Err(error)","error":"is Str(error)"}
	zerolog.MessageFieldName = LogMessageFieldName
	zerolog.ErrorFieldName = LogErrorFieldName
	zerolog.TimestampFunc = GTimeNow
	zerolog.TimestampFieldName = "T"
	zerolog.LevelFieldName = "L"
	zerolog.CallerFieldName = "F"
	zerolog.ErrorStackFieldName = "S"
	zerolog.DurationFieldInteger = true
	zerolog.InterfaceMarshalFunc = json.Marshal
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		fn := runtime.FuncForPC(pc).Name()
		i := strings.LastIndexByte(fn, '/')
		if i == -1 {
			return fn
		}
		return fn[i+1:] + ":" + strconv.Itoa(line)
	}
}

func loadLogger() error {
	alarmOn := config.AlarmOn.Load()
	if logAlarmOnConf != alarmOn {
		logAlarmOnConf = alarmOn
		if l := Log(); l != nil {
			l.Warn().Bool("alarm_on", logAlarmOnConf).Msg("Alarm switch changed")
		}
	}
	cfg := config.Config().LogConf
	if logCurrentConf == cfg {
		return nil
	}
	logCurrentConf = cfg

	if err := newLogger(); err != nil {
		return err
	}

	// 抽样的日志记录器
	sampler := &zerolog.BurstSampler{
		Burst:  cfg.Burst,
		Period: cfg.PeriodDuration,
	}
	newLogSampled := Log().Sample(&zerolog.LevelSampler{
		TraceSampler: sampler,
		DebugSampler: sampler,
		InfoSampler:  sampler,
		WarnSampler:  sampler,
		ErrorSampler: sampler,
	}).With().Bool("sampling", true).Logger()
	logSampled.Store(&newLogSampled)

	Log().Warn().Str("version", config.Version).Str("tz", config.DefaultTimeZone).
		Str("app_name", config.AppName).Str("bin_name", config.BinName).Str("deb_name", config.DebName).
		Int("cpus", runtime.NumCPU()).Int("procs", runtime.GOMAXPROCS(0)).
		Bool("alarm_on", logAlarmOnConf).Str("alarm_code", cfg.AlarmCode).Bool("app_sampler", AppLoggerUseSampler).
		Msg("Logger sssinitialized")
	return nil
}

// 日志配置
// 1. Debug 调试时, 日志高亮输出到控制台
// 2. 非调试模式时, 日志输出到文件(可选关闭高亮, 可选 JSON 输出, 保存最近 10 个 30 天内的日志)
func newLogger() (err error) {
	var wr io.Writer
	cfg := config.Config().LogConf
	wr = zerolog.ConsoleWriter{Out: os.Stdout, NoColor: cfg.NoColor, TimeFormat: LogTimeFormat}
	if !config.Debug {
		wr, err = lumberjack.NewRoller(
			cfg.File,
			// 以 MiB 为单位
			cfg.MaxSize*megabyte,
			&lumberjack.Options{
				// 以 天 为单位
				MaxAge:     time.Duration(cfg.MaxAge) * days,
				MaxBackups: cfg.MaxBackups,
				LocalTime:  true,
				Compress:   true,
			})
		if err != nil {
			return err
		}
		if !cfg.NoPretty {
			wr = zerolog.ConsoleWriter{Out: wr, NoColor: cfg.NoColor, TimeFormat: LogTimeFormat}
		}
	}

	newLog := zerolog.New(wr).With().Timestamp().Caller().Logger()
	newLog = newLog.Level(zerolog.Level(cfg.Level))
	logger.Store(&newLog)

	mw := zerolog.MultiLevelWriter(wr, logAlarmWriter)
	newLogAlarm := zerolog.New(mw).With().Timestamp().Caller().Logger()
	newLogAlarm = newLogAlarm.Level(zerolog.Level(cfg.Level))
	logAlarm.Store(&newLogAlarm)
	return nil
}

// 指定级别及以上日志发送到报警接口
type alarmWriter struct {
	lv  zerolog.Level
	fn  AlarmJsonGenerator
	off atomic.Bool
}

func newAlarmWriter(level zerolog.Level) *alarmWriter {
	return &alarmWriter{
		lv: level,
	}
}

// Write 发送报警消息到接口
func (w *alarmWriter) Write(p []byte) (n int, err error) {
	if logAlarmOnConf && !w.off.Load() {
		fn := w.fn
		if fn == nil {
			fn = genAlarmJson
		}
		bs := utils.CopyBytes(p)
		_ = ants.Submit(func() {
			sendAlarm(fn, bs)
		})
	}
	return len(p), nil
}

// WriteLevel 日志级别过滤
func (w *alarmWriter) WriteLevel(l zerolog.Level, p []byte) (n int, err error) {
	if l >= w.lv && l < zerolog.NoLevel {
		return w.Write(p)
	}
	return len(p), nil
}

type AppLogger struct {
	log func() *zerolog.Logger
}

// NewAppLogger 类库通用日志实现: Req / Ants / Gnet
func NewAppLogger(useSampler ...bool) *AppLogger {
	l := &AppLogger{
		log: Log,
	}
	if len(useSampler) == 0 && AppLoggerUseSampler || len(useSampler) > 0 && useSampler[0] {
		l.log = LogSampled
	}
	return l
}

func (l *AppLogger) Debugf(format string, v ...any) {
	l.log().Debug().Msgf(format, v...)
}

func (l *AppLogger) Infof(format string, v ...any) {
	l.log().Info().Msgf(format, v...)
}

func (l *AppLogger) Warnf(format string, v ...any) {
	l.log().Warn().Msgf(format, v...)
}

func (l *AppLogger) Printf(format string, v ...any) {
	l.log().Warn().Msgf(format, v...)
}

func (l *AppLogger) Errorf(format string, v ...any) {
	l.log().Error().Msgf(format, v...)
}

func (l *AppLogger) Fatalf(format string, v ...any) {
	l.log().Fatal().Msgf(format, v...)
}

type CronLogger struct {
	infoLog  func() *zerolog.Logger
	errorLog func() *zerolog.Logger
}

// NewCronLogger 定时任务日志
func NewCronLogger(useSampler ...bool) *CronLogger {
	l := &CronLogger{
		infoLog:  Log,
		errorLog: LogAlarm,
	}
	if len(useSampler) == 0 && AppLoggerUseSampler || len(useSampler) > 0 && useSampler[0] {
		l.infoLog = LogSampled
	}
	return l
}

func (l *CronLogger) Info(msg string, keysAndValues ...any) {
	l.infoLog().Info().Any("more", keysAndValues).Msg(msg)
}

func (l *CronLogger) Error(err error, msg string, keysAndValues ...any) {
	l.errorLog().Error().Err(err).Any("more", keysAndValues).Msg(msg)
}

type RedisLogger struct {
	log func() *zerolog.Logger
}

// NewRedisLogger go-redis 类库日志实现
func NewRedisLogger(useSampler ...bool) *RedisLogger {
	l := &RedisLogger{
		log: Log,
	}
	if len(useSampler) == 0 && AppLoggerUseSampler || len(useSampler) > 0 && useSampler[0] {
		l.log = LogSampled
	}
	return l
}

func (l *RedisLogger) Printf(_ context.Context, format string, v ...any) {
	l.log().Error().Msgf(format, v...)
}
