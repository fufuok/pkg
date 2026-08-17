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
		SetCommonRetryHook(retryRequestHook)
	if reqDebug == cfg.ReqDebug {
		return
	}
	reqDebug = cfg.ReqDebug
	Log().Warn().Bool("req_debug", reqDebug).Msg("Request debug switch changed")
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

// retryRequestHook 在默认客户端重试前记录状态码和 URL, 不写请求或响应正文.
// resp 在网络错误时可能没有底层 http.Response; 正文可能含密钥, 不能打进日志.
func retryRequestHook(resp *req.Response, err error) {
	ev := LogSampled().Warn().Err(err)
	if resp != nil {
		if resp.Response != nil {
			ev = ev.Int("status", resp.StatusCode)
		}
		if resp.Request != nil && resp.Request.RawURL != "" {
			ev = ev.Str("url", resp.Request.RawURL)
		}
	}
	ev.Msg("Retrying request")
}
