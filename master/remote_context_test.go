package master

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fufuok/pkg/config"
)

// TestStartRemoteConfCancelsInFlightFetch 验证热重载取消 ctx 时立即中断在途远端拉取并退出循环.
func TestStartRemoteConfCancelsInFlightFetch(t *testing.T) {
	prepareMasterConfig(t)
	registerCommonFuncs()
	remoteRandomWaitSeconds = func(int) int { return 0 }

	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := startRemoteConf(ctx, config.FilesConf{
		Method:          "GetDataSource",
		Path:            filepath.Join(t.TempDir(), "whitelist.conf"),
		API:             server.URL + "?token=",
		GetConfDuration: time.Hour,
	})

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote fetcher did not start HTTP request")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("remote fetcher did not exit promptly after context cancellation")
	}
}
