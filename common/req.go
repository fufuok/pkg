package common

import (
	"sync/atomic"
	"time"

	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/pools/timerpool"
)

const (
	// reqDebugBodyMaxLen ReqDebug 重试日志中请求/响应正文的最大打印字节数.
	reqDebugBodyMaxLen = 2048

	// defaultRetryInterval 保持与 req/v3 默认重试间隔一致 (100ms).
	defaultRetryInterval = 100 * time.Millisecond
)

var (
	// ReqUpload HTTP 文件上传客户端 (调试模式不显示上传文件内容, 无超时时间)
	ReqUpload *req.Client

	// ReqDownload HTTP 文件下载客户端 (调试模式不显示下载文件内容, 无超时时间)
	ReqDownload *req.Client

	// HTTP 客户端调试模式
	reqDebug atomic.Bool
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
	debug := cfg.ReqDebug
	req.SetTimeout(cfg.ReqTimeoutDuration).
		SetCommonRetryCount(cfg.ReqMaxRetries).
		SetCommonRetryCondition(retryRequestCondition).
		SetCommonRetryInterval(retryRequestInterval).
		SetCommonRetryHook(retryRequestHook)
	if reqDebug.Load() == debug {
		return
	}
	reqDebug.Store(debug)
	Log().Warn().Bool("req_debug", debug).Msg("Request debug switch changed")
	req.SetLogger(NewAppLogger())
	ReqUpload.SetLogger(NewAppLogger())
	ReqDownload.SetLogger(NewAppLogger())
	if debug {
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
// 事件必须在本函数内派发, 不能把未完成的 zerolog 事件返回给其他函数.
func retryRequestHook(resp *req.Response, err error) {
	// 同一事件的级别和正文使用入口状态, 避免热切换时混用两份配置.
	debug := reqDebug.Load()
	// 先选 logger 和级别再创建事件, 避免调试分支消耗共享 Warn/Error 采样额度.
	log, level := LogSampled(), zerolog.WarnLevel
	if debug {
		log, level = Log(), zerolog.NoLevel
	}
	ev := log.WithLevel(level).Err(err)
	if resp == nil {
		ev.Msg("Retrying request")
		return
	}
	if resp.Response != nil {
		ev = ev.Int("status", resp.StatusCode)
	}
	if resp.Request != nil {
		if resp.Request.RawURL != "" {
			ev = ev.Str("url", resp.Request.RawURL)
		}
		if body := reqDebugBody(resp.Request.Body, debug); body != "" {
			ev = ev.Str("req_body", body)
		}
	}
	if body := reqDebugBody(resp.Bytes(), debug); body != "" {
		ev = ev.Str("resp_body", body)
	}
	ev.Msg("Retrying request")
}

// reqDebugBody 仅在 ReqDebug 时返回截断后的正文, 空体或关闭调试时返回空串.
func reqDebugBody(body []byte, debug bool) string {
	if !debug || len(body) == 0 {
		return ""
	}
	if len(body) > reqDebugBodyMaxLen {
		body = body[:reqDebugBodyMaxLen]
	}
	return string(body)
}

// retryRequestCondition 保持与 req 默认条件一致(网络/传输错误时重试), 仅在调用方请求 Context 结束时停止重试.
// 不能用 errors.Is(err, context.DeadlineExceeded) 判断: http.Client.Timeout 的单次超时错误同样满足该判断,
// 而它正是需要重试的典型网络毛刺; 调用方 ctx 是否结束只能看 resp.Request.Context(), 单次超时不会使其结束.
// context.Canceled 已由 req 在调用条件前拦截, 此处无需重复判断.
func retryRequestCondition(resp *req.Response, err error) bool {
	if err == nil {
		return false
	}
	return resp == nil || resp.Request == nil || resp.Request.Context().Err() == nil
}

// retryRequestInterval 使默认重试退避响应请求 Context 取消, 避免 req 内部的 time.Sleep 阻塞已取消的调用方.
func retryRequestInterval(resp *req.Response, _ int) time.Duration {
	if resp == nil || resp.Request == nil {
		return defaultRetryInterval
	}
	ctx := resp.Request.Context()
	if ctx.Done() == nil {
		return defaultRetryInterval
	}
	if ctx.Err() != nil {
		return 0
	}
	timer := timerpool.New(defaultRetryInterval)
	defer timerpool.Release(timer)
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	return 0
}
