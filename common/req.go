package common

import (
	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
)

const (
	// reqDebugBodyMaxLen ReqDebug 重试日志中请求/响应正文的最大打印字节数.
	reqDebugBodyMaxLen = 2048
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

// loadReq 热更新默认客户端超时、重试和 ReqDebug.
// DefaultClient().Clone() 是快照, 本函数不会回写已有克隆体; 调用方要跟随配置变化必须重新 Clone.
//
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
		// 默认客户端 dump 头和正文, 方便开发调试; 敏感场景由调用方主动关 ReqDebug 或改用专用客户端.
		// dump 必须进 logger, 不能落到 stdout; 热开 ReqDebug 时生产进程标准输出通常无人收.
		// 上传/下载客户端仍分别隐藏文件体, 避免大文件或二进制内容刷屏.
		applyReqDebugDump(req.DefaultClient(), true, true)
		applyReqDebugDump(ReqUpload, false, true)
		applyReqDebugDump(ReqDownload, true, false)
		req.EnableDebugLog().EnableTraceAll()
		ReqUpload.EnableDebugLog().EnableTraceAll()
		ReqDownload.EnableDebugLog().EnableTraceAll()
	} else {
		req.DisableDumpAll().DisableDebugLog().DisableTraceAll()
		ReqUpload.DisableDumpAll().DisableDebugLog().DisableTraceAll()
		ReqDownload.DisableDumpAll().DisableDebugLog().DisableTraceAll()
	}
}

// newReq 重建默认客户端和上传/下载专用客户端.
// 调用方对 DefaultClient().Clone() 得到的是当时快照, 之后 loadReq 热更新超时、重试和 ReqDebug 不会回写到克隆体.
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

// applyReqDebugDump 打开指定客户端的 dump, 输出接到无级别 logger.
// requestBody/responseBody 为 false 时隐藏对应正文, 供上传/下载客户端复用.
func applyReqDebugDump(client *req.Client, requestBody, responseBody bool) {
	client.SetCommonDumpOptions(&req.DumpOptions{
		Output:         NewAppLoggerWriter(false),
		RequestHeader:  true,
		RequestBody:    requestBody,
		ResponseHeader: true,
		ResponseBody:   responseBody,
	})
	client.EnableDumpAll()
}

// retryRequestHook 在默认客户端重试前记录状态码和 URL.
// 非 ReqDebug 不写正文, 避免密钥进入抽样 Warn 日志; ReqDebug 时用无级别日志附截断正文.
// 成功请求走 dump 看完整正文; 重试路径仍限长, 避免失败体反复刷满日志.
// resp 在网络错误时可能没有底层 http.Response, 此时只保留 error 和已有 URL.
func retryRequestHook(resp *req.Response, err error) {
	ev := newRetryLogEvent().Err(err)
	if resp != nil {
		if resp.Response != nil {
			ev = ev.Int("status", resp.StatusCode)
		}
		if resp.Request != nil {
			if resp.Request.RawURL != "" {
				ev = ev.Str("url", resp.Request.RawURL)
			}
			if body := reqDebugBody(resp.Request.Body); body != "" {
				ev = ev.Str("req_body", body)
			}
		}
		if body := reqDebugBody(resp.Bytes()); body != "" {
			ev = ev.Str("resp_body", body)
		}
	}
	ev.Msg("Retrying request")
}

// newRetryLogEvent 选择重试日志通道.
// ReqDebug 用无级别事件, 不受默认 Warn 级别过滤; 生产仍走抽样 Warn.
func newRetryLogEvent() *zerolog.Event {
	if reqDebug {
		return Log().Log()
	}
	return LogSampled().Warn()
}

// reqDebugBody 仅在 ReqDebug 时返回截断后的正文, 空体或关闭调试时返回空串.
func reqDebugBody(body []byte) string {
	if !reqDebug || len(body) == 0 {
		return ""
	}
	if len(body) > reqDebugBodyMaxLen {
		body = body[:reqDebugBodyMaxLen]
	}
	return string(body)
}
