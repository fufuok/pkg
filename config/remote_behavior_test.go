package config

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/xhash"
)

// TestGetDataSourceBodyMatrix 验证 token 参数、成功响应、空结果和服务端错误.
func TestGetDataSourceBodyMatrix(t *testing.T) {
	requestTime := time.Unix(1_700_000_000, 0)
	secret := "source-secret"
	wantToken := xhash.MD5Hex(fmt.Sprintf("%d%s", requestTime.Unix(), secret))

	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
		wantErr    string
	}{
		{name: "success", statusCode: http.StatusOK, body: `{"ok":1,"data":[{"ip_info":"10.0.0.1"},{"ignored":"x"},{"ip_info":"10.0.0.2"}]}`, want: "10.0.0.1\n10.0.0.2"},
		{name: "empty result", statusCode: http.StatusOK, body: `{"ok":1,"data":[{"ignored":"x"}]}`, wantErr: "data source result is empty"},
		{name: "application error", statusCode: http.StatusOK, body: `{"ok":0,"msg":"denied"}`, wantErr: "data source request failed"},
		{name: "http error", statusCode: http.StatusBadGateway, body: `{"ok":1,"msg":"upstream"}`, wantErr: "data source request failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, wantToken, r.URL.Query().Get("token"))
				assert.Equal(t, fmt.Sprintf("%d", requestTime.Unix()), r.URL.Query().Get("time"))
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)

			got, err := GetDataSourceBody(DataSourceArgs{Time: requestTime, Conf: FilesConf{API: server.URL + "?token=", SecretValue: secret}})
			if tt.wantErr != "" {
				assert.True(t, err != nil)
				assert.Contains(t, tt.wantErr, err.Error())
				return
			}
			assert.Nil(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestGetDataSourceWithCheckMatrix 验证检查回调控制写入并保留错误上下文.
func TestGetDataSourceWithCheckMatrix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":1,"data":[{"ip_info":"line-one"}]}`))
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "source.conf")
	params := DataSourceArgs{Time: time.Unix(1, 0), Conf: FilesConf{Path: path, API: server.URL + "?token="}}

	assert.Nil(t, GetDataSourceWithCheck(params, func([]byte) (bool, error) { return false, nil }))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	assert.Nil(t, GetDataSourceWithCheck(params, func(body []byte) (bool, error) {
		assert.Equal(t, "line-one", string(body))
		return true, nil
	}))
	body, err := os.ReadFile(path)
	assert.Nil(t, err)
	assert.Equal(t, "line-one", string(body))

	err = GetDataSourceWithCheck(params, func([]byte) (bool, error) { return false, fmt.Errorf("check failed") })
	assert.True(t, err != nil)
	assert.Contains(t, "failed to check if update needed", err.Error())

	params.Conf.Path = filepath.Join(path, "child")
	err = GetDataSourceWithCheck(params, nil)
	assert.True(t, err != nil)
	assert.Contains(t, "failed to write config file", err.Error())
}

// TestGetDataSourcePublicContract 验证参数类型检查和基于文件 MD5 的更新入口.
func TestGetDataSourcePublicContract(t *testing.T) {
	err := GetDataSource("invalid")
	assert.True(t, err != nil)
	assert.Contains(t, "invalid data source configuration", err.Error())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":1,"data":[{"ip_info":"public-line"}]}`))
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "public.conf")
	params := DataSourceArgs{Time: time.Unix(2, 0), Conf: FilesConf{Path: path, API: server.URL + "?token="}}
	assert.Nil(t, GetDataSource(params))
	body, err := os.ReadFile(path)
	assert.Nil(t, err)
	assert.Equal(t, "public-line", strings.TrimSpace(string(body)))
}

// TestGetDataSourceBodyTransportError 验证网络失败由调用方获得原始请求错误.
func TestGetDataSourceBodyTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	_, err := GetDataSourceBody(DataSourceArgs{Time: time.Unix(3, 0), Conf: FilesConf{API: server.URL + "?token="}})
	assert.True(t, err != nil)
}

// TestGetDataSourceContextCancelAndAtomicWrite 验证取消中断在途拉取且不覆盖旧文件, 正常更新不留临时文件.
func TestGetDataSourceContextCancelAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitelist.conf")
	assert.Nil(t, os.WriteFile(path, []byte("10.0.0.1"), 0o600))

	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-r.Context().Done():
			return
		case <-release:
			_, _ = w.Write([]byte(`{"ok":1,"data":[{"ip_info":"10.0.0.2"}]}`))
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	params := DataSourceArgs{Time: time.Unix(4, 0), Conf: FilesConf{Path: path, API: server.URL + "?token="}}
	errCh := make(chan error, 1)
	go func() {
		errCh <- GetDataSourceContext(ctx, params)
	}()
	<-started
	cancel()
	err := <-errCh
	assert.True(t, errors.Is(err, context.Canceled))

	body, readErr := os.ReadFile(path)
	assert.Nil(t, readErr)
	assert.Equal(t, "10.0.0.1", string(body))

	entries, dirErr := os.ReadDir(dir)
	assert.Nil(t, dirErr)
	assert.Equal(t, 1, len(entries))
}

// TestWriteFileAtomicPreservesExistingFilePerm 验证覆盖已存在配置文件时继承其原有权限位.
func TestWriteFileAtomicPreservesExistingFilePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits are not preserved identically on Windows")
	}
	path := filepath.Join(t.TempDir(), "whitelist.conf")
	assert.Nil(t, os.WriteFile(path, []byte("10.0.0.1\n"), 0o640))
	assert.Nil(t, os.Chmod(path, 0o644))

	assert.Nil(t, writeFileAtomic(path, []byte("10.0.0.2\n"), 0o600))

	info, err := os.Stat(path)
	assert.Nil(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	body, err := os.ReadFile(path)
	assert.Nil(t, err)
	assert.Equal(t, "10.0.0.2\n", string(body))
}
