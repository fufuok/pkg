package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fufuok/pkg/config"
)

// TestInvokeConfigMethodContext 验证默认 GetDataSource 透传取消, 自定义覆盖方法保持原样调用.
func TestInvokeConfigMethodContext(t *testing.T) {
	prepareCommonConfig(t)

	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	Funcs.Store("GetDataSource", config.GetDataSource)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- InvokeConfigMethodContext(ctx, config.FilesConf{
			Method: "GetDataSource",
			Path:   filepath.Join(t.TempDir(), "remote.conf"),
			API:    server.URL + "?token=",
		})
	}()
	<-started
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("InvokeConfigMethodContext default err = %v, want context.Canceled", err)
	}

	called := false
	Funcs.Store("GetDataSource", func(args any) error {
		called = true
		if _, ok := args.(config.DataSourceArgs); !ok {
			t.Fatalf("unexpected args type: %T", args)
		}
		return nil
	})
	if err := InvokeConfigMethodContext(context.Background(), config.FilesConf{Method: "GetDataSource"}); err != nil {
		t.Fatalf("custom getter err = %v", err)
	}
	if !called {
		t.Fatal("custom getter override was bypassed")
	}
}
