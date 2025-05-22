package common

import (
	"github.com/imroc/req/v3"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
)

var (
	// ReqUpload HTTP 文件上传客户端 (调试模式不显示上传文件内容, 无超时时间)
	ReqUpload *req.Client

	// ReqDownload HTTP 文件下载客户端 (调试模式不显示下载文件内容, 无超时时间)
	ReqDownload *req.Client

	// HTTP 客户端调试模式
	reqDebug bool
)

func initReq() {
	newReq()
	loadReq()
}

//go:norace
func loadReq() {
	cfg := config.Config().SYSConf
	req.SetTimeout(cfg.ReqTimeoutDuration).
		SetCommonRetryCount(cfg.ReqMaxRetries).
		SetCommonRetryHook(func(resp *req.Response, err error) {
			LogSampled.Warn().Err(err).Str("resp", resp.String()).Msg("Retrying request")
		})
	if reqDebug == cfg.ReqDebug {
		return
	}
	reqDebug = cfg.ReqDebug
	Log.Warn().Bool("req_debug", reqDebug).Msg("Request debug switch changed")
	req.SetLogger(NewAppLogger())
	ReqUpload.SetLogger(NewAppLogger())
	ReqDownload.SetLogger(NewAppLogger())
	if reqDebug {
		req.EnableDumpAll().EnableDebugLog().EnableTraceAll()
		ReqUpload.EnableDumpAllWithoutRequestBody().EnableDebugLog().EnableTraceAll()
		ReqDownload.EnableDumpAllWithoutResponseBody().EnableDebugLog().EnableTraceAll()
	} else {
		req.DisableDumpAll().DisableDebugLog().DisableTraceAll()
		ReqUpload.DisableDumpAll().DisableDebugLog().DisableTraceAll()
		ReqDownload.DisableDumpAll().DisableDebugLog().DisableTraceAll()
	}
}

func newReq() {
	req.SetUserAgent(config.ReqUserAgent).
		SetJsonMarshal(json.Marshal).
		SetJsonUnmarshal(json.Unmarshal).
		SetLogger(NewAppLogger())
	ReqUpload = req.C().
		SetUserAgent(config.ReqUserAgent).
		SetJsonMarshal(json.Marshal).
		SetJsonUnmarshal(json.Unmarshal).
		SetLogger(NewAppLogger())
	ReqDownload = req.C().
		SetUserAgent(config.ReqUserAgent).
		SetJsonMarshal(json.Marshal).
		SetJsonUnmarshal(json.Unmarshal).
		SetLogger(NewAppLogger())
}
