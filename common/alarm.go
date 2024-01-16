package common

import (
	"os"

	"github.com/fufuok/ants"
	"github.com/fufuok/utils/xjson/gjson"
	"github.com/fufuok/utils/xjson/jsongen"
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/config"
)

const (
	logMoreFieldName = "more"
	logJobFieldName  = "job"
	errMsgMaxLength  = 300
)

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
	more := gjson.GetBytes(bs, logMoreFieldName).String()
	info := gjson.GetBytes(bs, logMessageFieldName).String()
	err := gjson.GetBytes(bs, logErrorFieldName).String()
	if err != "" {
		if len(err) > errMsgMaxLength {
			err = err[:300]
		}
		info += ": " + err
	}
	return GenAlarmJson(code, info, more)
}

// GenAlarmJson 整合报警消息
func GenAlarmJson(code, info, more string) []byte {
	now := Now()
	hostname, _ := os.Hostname()
	js := jsongen.NewMap()
	js.PutString("code", code)
	js.PutString("time", now.Str3339)
	js.PutString("info", info)
	js.PutString("more", more)
	js.PutString("hostname", hostname)
	js.PutInt("timestamp", now.Unix)
	return js.Serialize(nil)
}

// 发送报警消息
func sendAlarm(bs []byte) {
	cfg := config.Config().LogConf
	if cfg.PostAlarmAPI == "" || cfg.AlarmCode == "" {
		return
	}
	data := genAlarmJson(cfg.AlarmCode, bs)
	if _, err := req.SetBodyJsonBytes(data).Post(cfg.PostAlarmAPI); err != nil {
		LogSampled.Warn().Err(err).Str("url", cfg.PostAlarmAPI).Msg("sendAlarm")
	}
}

// 错误日志转换为报警信息
func genAlarmJson(code string, bs []byte) []byte {
	more := gjson.GetBytes(bs, logMoreFieldName).String()
	info := gjson.GetBytes(bs, logMessageFieldName).String()
	job := gjson.GetBytes(bs, logJobFieldName).String()
	err := gjson.GetBytes(bs, logErrorFieldName).String()
	if err != "" {
		if len(err) > errMsgMaxLength {
			err = err[:300]
		}
		info += ": " + err
	}

	cfg := config.Config().NodeConf.NodeInfo
	now := Now()
	js := jsongen.NewMap()
	js.PutString("code", code)
	js.PutString("node_ip", cfg.NodeIP)
	js.PutString("node_name", cfg.NodeName)
	js.PutString("node_desc", cfg.NodeDesc)
	js.PutString("hostname", cfg.Hostname)
	js.PutString("host_ip", cfg.HostIP)
	js.PutString("time", now.Str3339)
	js.PutString("info", info)
	js.PutString("more", more)
	js.PutString("job", job)
	js.PutInt("timestamp", now.Unix)
	return js.Serialize(nil)
}
