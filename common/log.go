package common

import (
	"context"
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
	// Log 通用日志, Debug 时输出到控制台, 否则写入日志文件
	Log zerolog.Logger

	// LogSampled 抽样日志
	LogSampled zerolog.Logger

	// LogAlarm 报警日志, 写入通用日志并发送报警
	LogAlarm zerolog.Logger

	logAlarmWriter = newAlarmWriter(zerolog.WarnLevel)
	logCurrentConf config.LogConf
	logAlarmOnConf bool
)

func initLogger() {
	initZerolog()
	logAlarmOnConf = config.AlarmOn.Load()
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
		Log.Warn().Bool("alarm_on", logAlarmOnConf).Msg("Alarm switch")
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
	LogSampled = Log.Sample(&zerolog.LevelSampler{
		TraceSampler: sampler,
		DebugSampler: sampler,
		InfoSampler:  sampler,
		WarnSampler:  sampler,
		ErrorSampler: sampler,
	}).With().Bool("sampling", true).Logger()

	Log.Warn().Str("version", config.Version).Str("tz", config.DefaultTimeZone).
		Str("app_name", config.AppName).Str("bin_name", config.BinName).Str("deb_name", config.DebName).
		Int("cpus", runtime.NumCPU()).Int("procs", runtime.GOMAXPROCS(0)).
		Bool("alarm_on", logAlarmOnConf).Str("alarm_code", cfg.AlarmCode).
		Msg("Logger initialized successfully")
	return nil
}

// 日志配置
// 1. 开发环境时, 日志高亮输出到控制台
// 2. 生产环境时, 日志输出到文件(可选关闭高亮, 保存最近 10 个 30 天内的日志)
func newLogger() error {
	cfg := config.Config().LogConf
	basicLog := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		NoColor:    cfg.NoColor,
		TimeFormat: LogTimeFormat,
	}
	if !config.Debug {
		fh, err := lumberjack.NewRoller(
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
		basicLog.Out = fh
	}

	Log = zerolog.New(basicLog).With().Timestamp().Caller().Logger()
	Log = Log.Level(zerolog.Level(cfg.Level))

	mw := zerolog.MultiLevelWriter(basicLog, logAlarmWriter)
	LogAlarm = zerolog.New(mw).With().Timestamp().Caller().Logger()
	LogAlarm = LogAlarm.Level(zerolog.Level(cfg.Level))
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
	log zerolog.Logger
}

// NewAppLogger 类库日志实现: Req / Ants
// 注意: 受抽样日志影响, 日志可能不会被全部输出
func NewAppLogger() *AppLogger {
	if config.Debug {
		return &AppLogger{
			log: Log,
		}
	}
	return &AppLogger{
		log: LogSampled,
	}
}

func (l *AppLogger) Debugf(format string, v ...any) {
	l.log.Debug().Msgf(format, v...)
}

func (l *AppLogger) Warnf(format string, v ...any) {
	l.log.Warn().Msgf(format, v...)
}

func (l *AppLogger) Printf(format string, v ...any) {
	l.log.Warn().Msgf(format, v...)
}

func (l *AppLogger) Errorf(format string, v ...any) {
	l.log.Error().Msgf(format, v...)
}

type CronLogger struct {
	infoLog  zerolog.Logger
	errorLog zerolog.Logger
}

// NewCronLogger 注意: 受抽样日志影响, 日志可能不会被全部输出
func NewCronLogger() *CronLogger {
	if config.Debug {
		return &CronLogger{
			infoLog:  Log,
			errorLog: LogAlarm,
		}
	}
	return &CronLogger{
		infoLog:  LogSampled,
		errorLog: LogAlarm,
	}
}

func (l *CronLogger) Info(msg string, keysAndValues ...any) {
	l.infoLog.Info().Any("more", keysAndValues).Msg(msg)
}

func (l *CronLogger) Error(err error, msg string, keysAndValues ...any) {
	l.errorLog.Error().Err(err).Any("more", keysAndValues).Msg(msg)
}

type RedisLogger struct {
	log zerolog.Logger
}

// NewRedisLogger go-redis 类库日志实现
func NewRedisLogger() *RedisLogger {
	if config.Debug {
		return &RedisLogger{
			log: Log,
		}
	}
	return &RedisLogger{
		log: LogSampled,
	}
}

func (l *RedisLogger) Printf(_ context.Context, format string, v ...any) {
	l.log.Error().Msgf(format, v...)
}
