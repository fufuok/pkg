package common

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

type retryHookSampler func(zerolog.Level) bool

// Sample 在事件创建时执行测试回调, 固定调试开关切换与正文读取的先后关系.
func (s retryHookSampler) Sample(level zerolog.Level) bool {
	return s(level)
}

// TestRetryHookDebugSnapshot 验证一次重试沿用入口的调试状态, 不混入后续启用的正文.
func TestRetryHookDebugSnapshot(t *testing.T) {
	cfg := prepareCommonConfig(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)
	cfg.SYSConf.ReqDebug = false
	newReq()
	loadReq()

	base := zerolog.New(&logs).Level(zerolog.WarnLevel).With().Bool("sampling", true).Logger()
	sampled := base.Sample(retryHookSampler(func(zerolog.Level) bool {
		// hook 已选择抽样通道; 在提取正文前通过真实配置加载启用调试.
		cfg.SYSConf.ReqDebug = true
		loadReq()
		return true
	}))
	logSampled.Store(&sampled)
	resp := &req.Response{
		Response: &http.Response{StatusCode: http.StatusBadGateway},
		Request:  &req.Request{RawURL: "http://example.invalid/retry", Body: []byte("REQUEST_BODY_MARKER")},
	}
	resp.SetBodyString("RESPONSE_BODY_MARKER")
	retryRequestHook(resp, errors.New("temporary failure"))

	got := logs.String()
	if strings.Count(got, "Retrying request") != 1 || !strings.Contains(got, `"sampling":true`) {
		t.Fatalf("retry event did not use the sampled logger: %s", got)
	}
	if strings.Contains(got, `"req_body"`) || strings.Contains(got, `"resp_body"`) {
		t.Fatalf("retry event used a later debug setting: %s", got)
	}
}

// TestRetryHookDebugConcurrent 用 race 检查真实配置加载与重试日志之间的开关同步.
func TestRetryHookDebugConcurrent(t *testing.T) {
	cfg := prepareCommonConfig(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.Disabled)
	newReq()
	loadReq()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10000 {
			retryRequestHook(nil, nil)
		}
	}()
	for i := range 500 {
		cfg.SYSConf.ReqDebug = i%2 == 0
		loadReq()
	}
	<-done
}

// TestRequestClientContract 验证 user-agent、超时、重试、debug 默认打印正文和专用客户端 body 隐藏策略.
func TestRequestClientContract(t *testing.T) {
	cfg := prepareCommonConfig(t)
	config.ReqUserAgent = "pkg-common-test/1.0"
	cfg.SYSConf.ReqDebug = true
	cfg.SYSConf.ReqTimeoutDuration = 50 * time.Millisecond
	cfg.SYSConf.ReqMaxRetries = 0
	var regularDump, uploadDump, downloadDump bytes.Buffer
	newReq()
	loadReq()
	// 生产路径会把 dump Output 接到 logger; EnableDumpAllTo 只换输出目标, 保留 loadReq 设好的头/体开关.
	req.DefaultClient().EnableDumpAllTo(&regularDump)
	ReqUpload.EnableDumpAllTo(&uploadDump)
	ReqDownload.EnableDumpAllTo(&downloadDump)

	if ReqUpload == nil || ReqDownload == nil {
		t.Fatal("specialized request clients were not initialized")
	}
	if got := req.DefaultClient().GetClient().Timeout; got != 50*time.Millisecond {
		t.Fatalf("default request timeout = %s", got)
	}
	uploadTimeout := ReqUpload.GetClient().Timeout
	downloadTimeout := ReqDownload.GetClient().Timeout
	if !reqDebug.Load() || !req.DefaultClient().DebugLog || !ReqUpload.DebugLog || !ReqDownload.DebugLog {
		t.Fatal("request debug mode was not enabled on all clients")
	}

	var retryCalls atomic.Int64
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			time.Sleep(150 * time.Millisecond)
			_, _ = w.Write([]byte("slow"))
		case "/retry":
			retryCalls.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case "/echo":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("regular response body"))
		default:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("DOWNLOAD_SECRET"))
		}
	}))
	attachReqTestServer(t, server, req.DefaultClient(), ReqUpload, ReqDownload)

	resp, err := req.Get(server.URL + "/ok")
	if err != nil {
		t.Fatalf("request local fixture: %v", err)
	}
	if got := resp.Request.RawRequest.Header.Get("User-Agent"); got != config.ReqUserAgent {
		t.Fatalf("user-agent = %q", got)
	}

	if _, err := req.SetBodyString("regular request body").Post(server.URL + "/echo"); err != nil {
		t.Fatalf("dump regular request: %v", err)
	}
	if _, err := ReqUpload.R().SetBodyString("UPLOAD_SECRET").Post(server.URL + "/echo"); err != nil {
		t.Fatalf("dump upload request: %v", err)
	}
	if _, err := ReqDownload.R().Get(server.URL + "/download"); err != nil {
		t.Fatalf("dump download request: %v", err)
	}
	if !strings.Contains(regularDump.String(), "POST /echo") || !strings.Contains(regularDump.String(), "200 OK") {
		t.Fatalf("regular debug dump omitted request or response metadata: %s", regularDump.String())
	}
	if !strings.Contains(regularDump.String(), "regular request body") {
		t.Fatal("regular debug dump omitted the request body")
	}
	if !strings.Contains(regularDump.String(), "regular response body") {
		t.Fatal("regular debug dump omitted the response body")
	}
	if strings.Contains(uploadDump.String(), "UPLOAD_SECRET") {
		t.Fatal("upload debug dump exposed the request body")
	}
	if !strings.Contains(uploadDump.String(), "POST /echo") || !strings.Contains(uploadDump.String(), "200 OK") {
		t.Fatalf("upload debug dump omitted request or response metadata: %s", uploadDump.String())
	}
	if strings.Contains(downloadDump.String(), "DOWNLOAD_SECRET") {
		t.Fatal("download debug dump exposed the response body")
	}
	if !strings.Contains(downloadDump.String(), "GET /download") || !strings.Contains(downloadDump.String(), "200 OK") {
		t.Fatalf("download debug dump omitted request or response metadata: %s", downloadDump.String())
	}

	cfg.SYSConf.ReqDebug = false
	loadReq()
	if reqDebug.Load() || req.DefaultClient().DebugLog || ReqUpload.DebugLog || ReqDownload.DebugLog {
		t.Fatal("request debug mode was not disabled on all clients")
	}
	dumpSize := regularDump.Len()
	if _, err := req.Get(server.URL + "/after-disable"); err != nil {
		t.Fatalf("request after disabling debug: %v", err)
	}
	if regularDump.Len() != dumpSize {
		t.Fatal("disabled debug mode continued writing dumps")
	}

	cfg.SYSConf.ReqTimeoutDuration = 20 * time.Millisecond
	cfg.SYSConf.ReqMaxRetries = 0
	loadReq()
	if ReqUpload.GetClient().Timeout != uploadTimeout || ReqDownload.GetClient().Timeout != downloadTimeout {
		t.Fatal("runtime reload overwrote a specialized client timeout")
	}
	started := time.Now()
	if _, err := req.Get(server.URL + "/slow"); err == nil {
		t.Fatal("request exceeding configured timeout succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request timeout took %s", elapsed)
	}

	cfg.SYSConf.ReqTimeoutDuration = time.Second
	cfg.SYSConf.ReqMaxRetries = 2
	loadReq()
	if _, err = req.Get(server.URL + "/retry"); err == nil {
		t.Fatal("connection reset unexpectedly succeeded")
	}
	if calls := retryCalls.Load(); calls != 3 {
		t.Fatalf("retry calls = %d, want 3", calls)
	}
}

// TestRetryHookDoesNotLogResponseBody 验证关闭 ReqDebug 时重试 hook 不写请求或响应正文.
func TestRetryHookDoesNotLogResponseBody(t *testing.T) {
	_ = prepareCommonConfig(t)
	reqDebug.Store(false)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)

	retryRequestHook(nil, errors.New("nil response"))
	if got := logs.String(); !strings.Contains(got, "Retrying request") || strings.Contains(got, "RETRY_SECRET_TOKEN") {
		t.Fatalf("nil response retry log = %s", got)
	}

	resp := &req.Response{
		Response: &http.Response{StatusCode: http.StatusBadGateway},
		Request:  &req.Request{RawURL: "http://127.0.0.1/retry", Body: []byte("REQ_SECRET_TOKEN")},
	}
	resp.SetBodyString("RETRY_SECRET_TOKEN")
	retryRequestHook(resp, errors.New("temporary failure"))

	got := logs.String()
	if !strings.Contains(got, "Retrying request") || !strings.Contains(got, `"status":502`) || !strings.Contains(got, "http://127.0.0.1/retry") {
		t.Fatalf("retry hook omitted status or url: %s", got)
	}
	if strings.Contains(got, "RETRY_SECRET_TOKEN") || strings.Contains(got, "REQ_SECRET_TOKEN") {
		t.Fatalf("retry hook logged request or response body: %s", got)
	}
}

// TestRetryHookLogsTruncatedBodyWhenReqDebug 验证 ReqDebug 下重试 hook 打印截断正文.
// 成功请求不走 hook; 超长正文只保留前 reqDebugBodyMaxLen 字节.
func TestRetryHookLogsTruncatedBodyWhenReqDebug(t *testing.T) {
	_ = prepareCommonConfig(t)
	reqDebug.Store(true)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)

	reqTail := "REQ_SECRET_TAIL"
	respTail := "RETRY_SECRET_TAIL"
	resp := &req.Response{
		Response: &http.Response{StatusCode: http.StatusBadGateway},
		Request: &req.Request{
			RawURL: "http://127.0.0.1/retry",
			Body:   []byte(strings.Repeat("B", reqDebugBodyMaxLen) + reqTail),
		},
	}
	resp.SetBodyString(strings.Repeat("A", reqDebugBodyMaxLen) + respTail)
	retryRequestHook(resp, errors.New("temporary failure"))

	got := logs.String()
	if !strings.Contains(got, "Retrying request") || !strings.Contains(got, `"status":502`) || !strings.Contains(got, "http://127.0.0.1/retry") {
		t.Fatalf("req debug retry hook omitted status or url: %s", got)
	}
	if !strings.Contains(got, `"req_body"`) || !strings.Contains(got, `"resp_body"`) {
		t.Fatalf("req debug retry hook omitted truncated bodies: %s", got)
	}
	if strings.Contains(got, reqTail) || strings.Contains(got, respTail) {
		t.Fatalf("req debug retry hook did not truncate bodies: %s", got)
	}
}

// TestRetryHookSamplingBudget 验证调试重试不占业务采样额度, 非调试重试仍正常采样.
// 使用真实且由 Warn/Error 共用的 BurstSampler, 避免仅替换 writer 漏掉事件创建时的副作用.
func TestRetryHookSamplingBudget(t *testing.T) {
	oldLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(oldLevel) })
	for _, debug := range []bool{false, true} {
		t.Run(strconv.FormatBool(debug), func(t *testing.T) {
			preserveCommonPackageState(t)
			// 固定采样时钟, 让两次重试和业务错误始终处于同一采样周期.
			zerolog.TimestampFunc = func() time.Time { return time.Unix(1, 0) }
			var logs lockedBuffer
			base := zerolog.New(&logs).Level(zerolog.WarnLevel)
			burst := &zerolog.BurstSampler{Burst: 2, Period: time.Hour}
			sampled := base.Sample(&zerolog.LevelSampler{WarnSampler: burst, ErrorSampler: burst})
			logger.Store(&base)
			logSampled.Store(&sampled)
			reqDebug.Store(debug)

			retryRequestHook(nil, errors.New("temporary failure"))
			retryRequestHook(nil, errors.New("temporary failure"))
			LogSampled().Error().Msg("Business error after retries")

			got := logs.String()
			if count := strings.Count(got, "Retrying request"); count != 2 {
				t.Fatalf("retry logs = %d, want 2", count)
			}
			if visible := strings.Contains(got, "Business error after retries"); visible != debug {
				t.Fatalf("business error visible = %t, want %t after debug=%t retries", visible, debug, debug)
			}
		})
	}
}

// TestReqDebugDumpWritesToLogger 验证 ReqDebug dump 写入 logger, 不落到 stdout, 且默认客户端打印正文.
func TestReqDebugDumpWritesToLogger(t *testing.T) {
	cfg := prepareCommonConfig(t)
	cfg.SYSConf.ReqDebug = true
	cfg.SYSConf.ReqMaxRetries = 0
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)
	AppLoggerUseSampler = false

	newReq()
	loadReq()

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	attachReqTestServer(t, server, req.DefaultClient())

	if _, err := req.R().SetBodyString("logger request body").Post(server.URL + "/dump-logger"); err != nil {
		t.Fatalf("request dump logger fixture: %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, "POST /dump-logger") || !strings.Contains(got, "200 OK") {
		t.Fatalf("req debug dump did not write headers to logger: %s", got)
	}
	if !strings.Contains(got, "logger request body") {
		t.Fatalf("req debug dump omitted the request body: %s", got)
	}
	if !strings.Contains(got, "ok") {
		t.Fatalf("req debug dump omitted the response body: %s", got)
	}
}

// TestClonedDefaultClientKeepsSnapshotAfterReload 验证 Clone 只拷当时配置, loadReq 热更新不会回写克隆体.
func TestClonedDefaultClientKeepsSnapshotAfterReload(t *testing.T) {
	cfg := prepareCommonConfig(t)
	cfg.SYSConf.ReqTimeoutDuration = 40 * time.Millisecond
	cfg.SYSConf.ReqMaxRetries = 0
	cfg.SYSConf.ReqDebug = false
	newReq()
	loadReq()

	cloned := req.DefaultClient().Clone()
	if got := cloned.GetClient().Timeout; got != 40*time.Millisecond {
		t.Fatalf("cloned timeout = %s, want 40ms", got)
	}

	cfg.SYSConf.ReqTimeoutDuration = 80 * time.Millisecond
	cfg.SYSConf.ReqDebug = true
	loadReq()
	if got := req.DefaultClient().GetClient().Timeout; got != 80*time.Millisecond {
		t.Fatalf("default timeout after reload = %s, want 80ms", got)
	}
	if got := cloned.GetClient().Timeout; got != 40*time.Millisecond {
		t.Fatalf("cloned timeout followed loadReq: %s", got)
	}
	if cloned.DebugLog {
		t.Fatal("cloned client unexpectedly inherited later ReqDebug")
	}
}

// attachReqTestServer 把当前测试拥有的 req 客户端接入服务器的 HTTP 内存网络.
// 只替换拨号并关闭环境代理, 保留 req transport 的 dump、超时和重试路径.
// 必须在请求前调用; NewTestServer 负责关闭服务器, 本函数负责关闭 req 的空闲连接.
func attachReqTestServer(t *testing.T, server *httptest.Server, clients ...*req.Client) {
	t.Helper()
	transport, ok := server.Client().Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatal("test server client does not provide an HTTP dialer")
	}
	for _, client := range clients {
		client.SetProxy(nil).SetDial(transport.DialContext)
		t.Cleanup(client.Transport.CloseIdleConnections)
	}
}
