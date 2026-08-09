package common

import (
	"testing"

	"github.com/fufuok/cache/xsync"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// preserveCommonPackageState 隔离 common 测试触达的 logger、req、Redis 和队列状态.
//
// common 对外暴露多个进程级单例. 相关测试必须串行执行并在修改前调用本函数;
// cleanup 会先收敛遗留的公开测试助手, 再恢复调用前对象身份和第三方库全局配置.
func preserveCommonPackageState(t *testing.T) {
	t.Helper()
	if commonTestState != nil {
		t.Fatal("common test helper must be stopped before preserving package state")
	}

	oldInternalIP := InternalIPv4
	oldExternalIP := ExternalIPv4
	oldInternalIPv4Lookup := internalIPv4Lookup
	oldExternalIPv4Lookup := externalIPv4Lookup
	oldAppLoggerUseSampler := AppLoggerUseSampler
	oldLogger := logger.Load()
	oldLogSampled := logSampled.Load()
	oldLogAlarm := logAlarm.Load()
	oldLogAlarmWriter := logAlarmWriter
	oldLogCurrentConf := logCurrentConf
	oldLogAlarmOnConf := logAlarmOnConf
	oldDisabledLogger := disabledLogger
	oldLogChan := LogChan
	oldPostAPI := postAPI
	oldReqUpload := ReqUpload
	oldReqDownload := ReqDownload
	oldReqDebug := reqDebug
	oldReqDefault := req.DefaultClient()
	oldRedisDB := RedisDB
	oldRedisInited := RedisDBInited.Load()
	oldStartTime := StartTime
	oldClockOffsetLimit := ClockOffsetLimit
	oldClockOffsetAdjust := ClockOffsetAdjust
	oldClockOffsetMinInterval := ClockOffsetMinInterval
	oldClockOffsetInterval := ClockOffsetInterval
	oldClockOffset := clockOffset.Load()
	oldFuncs := Funcs
	oldMaxGoPool := MaxGoPool
	oldErrMsgMaxLength := ErrMsgMaxLength

	oldMessageFieldName := zerolog.MessageFieldName
	oldErrorFieldName := zerolog.ErrorFieldName
	oldTimestampFunc := zerolog.TimestampFunc
	oldTimestampFieldName := zerolog.TimestampFieldName
	oldLevelFieldName := zerolog.LevelFieldName
	oldCallerFieldName := zerolog.CallerFieldName
	oldErrorStackFieldName := zerolog.ErrorStackFieldName
	oldDurationFieldInteger := zerolog.DurationFieldInteger
	oldInterfaceMarshalFunc := zerolog.InterfaceMarshalFunc
	oldCallerMarshalFunc := zerolog.CallerMarshalFunc

	logger.Store(nil)
	logSampled.Store(nil)
	logAlarm.Store(nil)
	logAlarmWriter = newAlarmWriter(zerolog.WarnLevel)
	logCurrentConf = config.LogConf{}
	logAlarmOnConf = false
	LogChan = nil
	postAPI = ""
	ReqUpload = nil
	ReqDownload = nil
	reqDebug = false
	req.SetDefaultClient(req.C())
	InitRedisDB(nil)
	clockOffset.Store(0)
	Funcs = xsync.NewMap[string, Func]()
	internalIPv4Lookup = oldInternalIPv4Lookup
	externalIPv4Lookup = oldExternalIPv4Lookup

	t.Cleanup(func() {
		if commonTestState != nil {
			StopTester()
		}
		InternalIPv4 = oldInternalIP
		ExternalIPv4 = oldExternalIP
		internalIPv4Lookup = oldInternalIPv4Lookup
		externalIPv4Lookup = oldExternalIPv4Lookup
		AppLoggerUseSampler = oldAppLoggerUseSampler
		logger.Store(oldLogger)
		logSampled.Store(oldLogSampled)
		logAlarm.Store(oldLogAlarm)
		logAlarmWriter = oldLogAlarmWriter
		logCurrentConf = oldLogCurrentConf
		logAlarmOnConf = oldLogAlarmOnConf
		disabledLogger = oldDisabledLogger
		LogChan = oldLogChan
		postAPI = oldPostAPI
		ReqUpload = oldReqUpload
		ReqDownload = oldReqDownload
		reqDebug = oldReqDebug
		req.SetDefaultClient(oldReqDefault)
		RedisDB = oldRedisDB
		RedisDBInited.Store(oldRedisInited)
		StartTime = oldStartTime
		ClockOffsetLimit = oldClockOffsetLimit
		ClockOffsetAdjust = oldClockOffsetAdjust
		ClockOffsetMinInterval = oldClockOffsetMinInterval
		ClockOffsetInterval = oldClockOffsetInterval
		clockOffset.Store(oldClockOffset)
		Funcs = oldFuncs
		MaxGoPool = oldMaxGoPool
		ErrMsgMaxLength = oldErrMsgMaxLength

		zerolog.MessageFieldName = oldMessageFieldName
		zerolog.ErrorFieldName = oldErrorFieldName
		zerolog.TimestampFunc = oldTimestampFunc
		zerolog.TimestampFieldName = oldTimestampFieldName
		zerolog.LevelFieldName = oldLevelFieldName
		zerolog.CallerFieldName = oldCallerFieldName
		zerolog.ErrorStackFieldName = oldErrorStackFieldName
		zerolog.DurationFieldInteger = oldDurationFieldInteger
		zerolog.InterfaceMarshalFunc = oldInterfaceMarshalFunc
		zerolog.CallerMarshalFunc = oldCallerMarshalFunc
	})
}
