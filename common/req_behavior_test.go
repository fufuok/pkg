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
	setRequestDumpOptions(req.DefaultClient(), &regularDump)
	setRequestDumpOptions(ReqUpload, &uploadDump)
	setRequestDumpOptions(ReqDownload, &downloadDump)
	loadReq()

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

// TestRetryHookDoesNotLogResponseBody 验证重试 hook 不把响应正文写入抽样日志.
// NodeAgent / DataRouter / DataPlugins 默认客户端走 loadReq, 当前 hook 直接打 resp.String().
func TestRetryHookDoesNotLogResponseBody(t *testing.T) {
	_ = prepareCommonConfig(t)
	var logs lockedBuffer
	installCommonTestLoggers(&logs, zerolog.WarnLevel)

	retryRequestHook(nil, errors.New("nil response"))
	if got := logs.String(); !strings.Contains(got, "Retrying request") || strings.Contains(got, "RETRY_SECRET_TOKEN") {
		t.Fatalf("nil response retry log = %s", got)
	}

	resp := &req.Response{
		Response: &http.Response{StatusCode: http.StatusBadGateway},
		Request:  &req.Request{RawURL: "http://127.0.0.1/retry"},
	}
	resp.SetBodyString("RETRY_SECRET_TOKEN")
	retryRequestHook(resp, errors.New("temporary failure"))

	got := logs.String()
	if !strings.Contains(got, "Retrying request") || !strings.Contains(got, `"status":502`) || !strings.Contains(got, "http://127.0.0.1/retry") {
		t.Fatalf("retry hook omitted status or url: %s", got)
	}
	if strings.Contains(got, "RETRY_SECRET_TOKEN") {
		t.Fatalf("retry hook logged response body: %s", got)
	}
}

// setRequestDumpOptions 把 req 调试输出定向到内存, 保留四类 header/body 开关.
func setRequestDumpOptions(client *req.Client, output *bytes.Buffer) {
	client.SetCommonDumpOptions(&req.DumpOptions{
		Output:         output,
		RequestHeader:  true,
		RequestBody:    true,
		ResponseHeader: true,
		ResponseBody:   true,
	})
}
