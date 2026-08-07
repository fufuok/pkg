package common

import (
	"testing"
	"time"

	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/config"
)

// TestTesterLifecycle 验证助手离线初始化、重复执行和 pool 所有权恢复.
func TestTesterLifecycle(t *testing.T) {
	oldConfig := config.Config()
	oldInternalIP, oldExternalIP := InternalIPv4, ExternalIPv4

	for range 2 {
		InitTester()
		if config.Config() == nil {
			t.Fatal("tester did not initialize owned config")
		}
		if InternalIPv4 != "127.0.0.1" || ExternalIPv4 != "127.0.0.1" {
			t.Fatalf("tester IPs = %q/%q", InternalIPv4, ExternalIPv4)
		}
		assertDefaultPoolSubmit(t)

		StopTester()
		if config.Config() != oldConfig {
			t.Fatal("tester did not restore owned config")
		}
		if InternalIPv4 != oldInternalIP || ExternalIPv4 != oldExternalIP {
			t.Fatal("tester did not restore IP state")
		}
		assertDefaultPoolSubmit(t)
	}
}

// TestTesterRejectsNestedInit 冻结助手只支持串行 Init-Stop 的边界.
func TestTesterRejectsNestedInit(t *testing.T) {
	InitTester()
	t.Cleanup(StopTester)

	defer func() {
		if recover() == nil {
			t.Fatal("nested InitTester should panic")
		}
	}()
	InitTester()
}

// assertDefaultPoolSubmit 验证当前默认池仍可接收并执行任务.
func assertDefaultPoolSubmit(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	if err := ants.Submit(func() { close(done) }); err != nil {
		t.Fatalf("submit task to default pool: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for default pool task")
	}
}
