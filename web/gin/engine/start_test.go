package engine

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkg/config"
)

// TestRunFailsFast 用真实子进程验证任一监听或分组构建失败都会退出,
// 避免 log.Fatal 终止测试进程, 也覆盖成功监听阻塞 WaitGroup.Wait 的组合场景.
func TestRunFailsFast(t *testing.T) {
	if mode := os.Getenv("PKG_WEB_START_TEST"); mode != "" {
		config.RootPath = filepath.Join(os.Getenv("PKG_WEB_START_ROOT"), "bin")
		config.AppBaseSecretValue = "Tester"
		config.AppConfigBody = []byte(`{}`)
		if err := new(config.M).Start(); err != nil {
			t.Fatal(err)
		}

		setup := func(app *gin.Engine) *gin.Engine { return app }
		cfg := config.WebConf{Name: "test-api", ServerAddr: "127.0.0.1:0"}
		if strings.HasSuffix(mode, "tls") {
			cfg.ServerHttpsAddr = "127.0.0.1:0"
			cfg.CertFile = filepath.Join(config.RootPath, "missing-cert.pem")
			cfg.KeyFile = filepath.Join(config.RootPath, "missing-key.pem")
		} else {
			cfg.ServerAddr += "," + os.Getenv("PKG_WEB_START_ADDR")
		}
		if mode == "groups-config" {
			RunGroups(
				ServerGroup{Name: "healthy", WebConf: config.WebConf{ServerAddr: "127.0.0.1:0"}, Setup: setup},
				ServerGroup{Name: "invalid", WebConf: config.WebConf{Name: "test-api", TrustedProxies: []string{"invalid-proxy"}}, Setup: setup},
			)
		} else if strings.HasPrefix(mode, "groups-") {
			cfg.ServerAddr = ""
			if strings.HasSuffix(mode, "listen") {
				cfg.ServerAddr = os.Getenv("PKG_WEB_START_ADDR")
			}
			RunGroups(
				ServerGroup{Name: "healthy", WebConf: config.WebConf{ServerAddr: "127.0.0.1:0"}, Setup: setup},
				ServerGroup{Name: "api", WebConf: cfg, Setup: setup},
			)
		} else {
			Run(setup, cfg)
		}
		t.Fatal("server returned without exiting")
		return
	}

	for _, mode := range []string{"run-listen", "groups-listen", "run-tls", "groups-tls", "groups-config"} {
		t.Run(mode, func(t *testing.T) {
			// 父进程持有冲突端口, 不使用先关闭临时监听再抢占地址的竞态做法.
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunFailsFast$")
			cmd.Env = append(os.Environ(), "PKG_WEB_START_TEST="+mode,
				"PKG_WEB_START_ADDR="+occupied.Addr().String(), "PKG_WEB_START_ROOT="+t.TempDir())
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("expected immediate exit 1, got %v (context: %v):\n%s", err, ctx.Err(), output)
			}
			if !strings.Contains(string(output), "Failed to start HTTP") || !strings.Contains(string(output), "test-api") {
				t.Fatalf("missing server failure context:\n%s", output)
			}
			if strings.HasSuffix(mode, "listen") && !strings.Contains(string(output), occupied.Addr().String()) {
				t.Fatalf("missing failed listener address:\n%s", output)
			}
		})
	}
}
