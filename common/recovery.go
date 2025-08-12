package common

import (
	"github.com/fufuok/utils"
)

var (
	_ utils.RecoveryCallback = RecoverAlarm
	_ utils.RecoveryCallback = RecoverLogger
)

// RecoverAlarm 记录崩溃日志并发出报警
func RecoverAlarm(err interface{}, trace []byte) {
	info := utils.MustString(err)
	more := utils.MustString(trace)
	Log().Error().Str("error", info).Str("trace", more).Msg("Recovered and triggered alarm")
	SendAlarm("", info, more)
}

// RecoverLogger 记录崩溃日志
func RecoverLogger(err interface{}, trace []byte) {
	info := utils.MustString(err)
	more := utils.MustString(trace)
	Log().Error().Str("error", info).Str("trace", more).Msg("Recovered from panic")
}
