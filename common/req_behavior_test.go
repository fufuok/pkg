package common

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/config"
)

// TestRequestClientContract 验证 user-agent、超时、重试、debug 和专用客户端 body 隐藏策略.
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
	if !reqDebug || !req.DefaultClient().DebugLog || !ReqUpload.DebugLog || !ReqDownload.DebugLog {
		t.Fatal("request debug mode was not enabled on all clients")
	}

	var retryCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		default:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("DOWNLOAD_SECRET"))
		}
	}))
	t.Cleanup(server.Close)

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
	if strings.Contains(regularDump.String(), "regular request body") {
		t.Fatal("regular debug dump exposed the request body")
	}
	if strings.Contains(regularDump.String(), "DOWNLOAD_SECRET") {
		t.Fatal("regular debug dump exposed the response body")
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
	if reqDebug || req.DefaultClient().DebugLog || ReqUpload.DebugLog || ReqDownload.DebugLog {
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
	reqDebug = false
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
	reqDebug = true
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

// TestReqDebugDumpWritesToLogger 验证 ReqDebug dump 写入 logger, 不落到 stdout.
func TestReqDebugDumpWritesToLogger(t *testing.T) {
	cfg := prepareCommonConfig(t)
	cfg.SYSConf.ReqDebug = true
	cfg.SYSConf.ReqMaxRetries = 0
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)
	AppLoggerUseSampler = false

	newReq()
	loadReq()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	if _, err := req.Get(server.URL + "/dump-logger"); err != nil {
		t.Fatalf("request dump logger fixture: %v", err)
	}
	got := logs.String()
	if !strings.Contains(got, "GET /dump-logger") || !strings.Contains(got, "200 OK") {
		t.Fatalf("req debug dump did not write headers to logger: %s", got)
	}
	if strings.Contains(got, "\nok\n") || strings.Contains(got, "\"ok\"") {
		t.Fatalf("req debug dump exposed the response body: %s", got)
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
