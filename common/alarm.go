package common

import (
	"os"
	"time"

	"github.com/fufuok/ants"
	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xjson/gjson"
	"github.com/fufuok/utils/xjson/jsongen"
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

const (
	LogMoreFieldName = "more"
	LogJobFieldName  = "job"
)

// ErrMsgMaxLength 日志中错误消息字段转换为报警消息的最大长度
var ErrMsgMaxLength = 300

// AlarmJsonGenerator 报警消息 JSON 生成函数, 入参: 报警平台 code, Log 日志
type AlarmJsonGenerator func(code string, bs []byte) []byte

// SetAlarmLevel 初始化设置警报日志的级别
func SetAlarmLevel(level zerolog.Level) {
	logAlarmWriter.lv = level
}

// SetAlarmFunc 初始化报警消息 Json 生成函数, 不设置时使用系统默认
func SetAlarmFunc(fn AlarmJsonGenerator) {
	logAlarmWriter.fn = fn
}

// SetAlarmOn 开启警报(发出报警消息, 默认开启)
func SetAlarmOn() {
	logAlarmWriter.off.Store(false)
}

// SetAlarmOff 关闭警报, 仅记录日志
func SetAlarmOff() {
	logAlarmWriter.off.Store(true)
}

// SendAlarm 发送自定义报警消息
func SendAlarm(code, info, more string) {
	cfg := config.Config().LogConf
	if code == "" {
		code = cfg.AlarmCode
	}
	if cfg.PostAlarmAPI == "" || code == "" {
		LogSampled.Warn().Str("info", info).Str("more", more).Msg("SendAlarm")
		return
	}
	data := GenAlarmJson(code, info, more)
	_ = ants.Submit(func() {
		if _, err := req.SetBodyJsonBytes(data).Post(cfg.PostAlarmAPI); err != nil {
			LogSampled.Warn().Err(err).Str("url", cfg.PostAlarmAPI).Msg("SendAlarm")
		}
	})
}

// GenAlarmData 错误日志转换为报警信息
func GenAlarmData(code string, bs []byte) []byte {
	more := gjson.GetBytes(bs, LogMoreFieldName).String()
	info := gjson.GetBytes(bs, LogMessageFieldName).String()
	err := gjson.GetBytes(bs, LogErrorFieldName).String()
	if err != "" {
		info += ": " + utils.TruncStr(err, ErrMsgMaxLength, "..")
	}
	return GenAlarmJson(code, info, more)
}

// GenAlarmJson 整合报警消息
func GenAlarmJson(code, info, more string) []byte {
	hostname, _ := os.Hostname()
	js := jsongen.NewMap()
	js.PutString("code", code)
	js.PutString("time", GTimeNowString(time.RFC3339))
	js.PutString("info", info)
	js.PutString("more", more)
	js.PutString("hostname", hostname)
	return js.Serialize(nil)
}

// 发送报警消息
func sendAlarm(fn AlarmJsonGenerator, bs []byte) {
	cfg := config.Config().LogConf
	if cfg.PostAlarmAPI == "" || cfg.AlarmCode == "" {
		return
	}
	data := fn(cfg.AlarmCode, bs)
	if _, err := req.SetBodyJsonBytes(data).Post(cfg.PostAlarmAPI); err != nil {
		LogSampled.Warn().Err(err).Str("url", cfg.PostAlarmAPI).Msg("sendAlarm")
	}
}

// 错误日志转换为报警信息
func genAlarmJson(code string, bs []byte) []byte {
	more := gjson.GetBytes(bs, LogMoreFieldName).String()
	info := gjson.GetBytes(bs, LogMessageFieldName).String()
	job := gjson.GetBytes(bs, LogJobFieldName).String()
	err := gjson.GetBytes(bs, LogErrorFieldName).String()
	if err != "" {
		info += ": " + utils.TruncStr(err, ErrMsgMaxLength, "..")
	}

	cfg := config.Config().NodeConf.NodeInfo
	js := jsongen.NewMap()
	js.PutString("code", code)
	js.PutString("node_ip", cfg.NodeIP)
	js.PutString("node_name", cfg.NodeName)
	js.PutString("node_desc", cfg.NodeDesc)
	js.PutString("hostname", cfg.Hostname)
	js.PutString("host_ip", cfg.HostIP)
	js.PutString("time", GTimeNowString(time.RFC3339))
	js.PutString("info", info)
	js.PutString("more", more)
	js.PutString("job", job)
	return js.Serialize(nil)
}
