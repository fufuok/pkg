package common

import (
	"testing"
	"time"

	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/config"
)

// TestTesterLifecycle 验证助手离线初始化、重复执行和 pool 所有权恢复.
func TestTesterLifecycle(t *testing.T) {
	sentinelPool := installSentinelDefaultPool(t)
	oldConfig := config.Config()
	oldInternalIP, oldExternalIP := InternalIPv4, ExternalIPv4

	for range 2 {
		InitTester()
		t.Cleanup(StopTester)
		if config.Config() == nil {
			t.Fatal("tester did not initialize owned config")
		}
		if InternalIPv4 != "127.0.0.1" || ExternalIPv4 != "127.0.0.1" {
			t.Fatalf("tester IPs = %q/%q", InternalIPv4, ExternalIPv4)
		}
		helperPool := commonTestState.helperPool
		assertDefaultPoolIdentity(t, helperPool)
		assertDefaultPoolSubmit(t)

		StopTester()
		if !helperPool.IsClosed() {
			t.Fatal("tester did not release helper pool")
		}
		assertDefaultPoolIdentity(t, sentinelPool)
		if config.Config() != oldConfig {
			t.Fatal("tester did not restore owned config")
		}
		if InternalIPv4 != oldInternalIP || ExternalIPv4 != oldExternalIP {
			t.Fatal("tester did not restore IP state")
		}
		assertDefaultPoolSubmit(t)
	}
}

// TestTesterPreservesCallerConfig 验证 common 不会接管调用方预先初始化的配置.
func TestTesterPreservesCallerConfig(t *testing.T) {
	sentinelPool := installSentinelDefaultPool(t)
	config.InitTester()
	t.Cleanup(config.StopTester)
	callerConfig := config.Config()

	InitTester()
	t.Cleanup(StopTester)
	if commonTestState.configOwned {
		t.Fatal("tester unexpectedly took ownership of caller config")
	}
	helperPool := commonTestState.helperPool
	StopTester()

	if config.Config() != callerConfig || !config.ConfigInitialized {
		t.Fatal("tester did not preserve caller-owned config")
	}
	if !helperPool.IsClosed() {
		t.Fatal("tester did not release helper pool")
	}
	assertDefaultPoolIdentity(t, sentinelPool)
	assertDefaultPoolSubmit(t)
}

// TestTesterDoesNotReleaseReplacementPool 验证助手只释放自己创建的 pool.
func TestTesterDoesNotReleaseReplacementPool(t *testing.T) {
	sentinelPool := installSentinelDefaultPool(t)
	InitTester()
	t.Cleanup(StopTester)
	helperPool := commonTestState.helperPool

	replacementPool, err := ants.NewPool(1)
	if err != nil {
		t.Fatalf("create caller replacement pool: %v", err)
	}
	t.Cleanup(replacementPool.Release)
	if displaced := ants.SwapDefaultAntsPool(replacementPool); displaced != helperPool {
		t.Fatal("caller replacement did not displace helper pool")
	}

	StopTester()
	if !helperPool.IsClosed() {
		t.Fatal("tester did not release owned helper pool")
	}
	if replacementPool.IsClosed() {
		t.Fatal("tester released caller-owned replacement pool")
	}
	assertDefaultPoolIdentity(t, sentinelPool)
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

// installSentinelDefaultPool 安装可识别的调用方 pool, 并在测试结束时恢复原默认池.
func installSentinelDefaultPool(t *testing.T) ants.Pooler {
	t.Helper()
	sentinelPool, err := ants.NewPool(1)
	if err != nil {
		t.Fatalf("create sentinel pool: %v", err)
	}
	previousPool := ants.SwapDefaultAntsPool(sentinelPool)
	if previousPool == nil {
		sentinelPool.Release()
		t.Fatal("preserve original default pool")
	}
	t.Cleanup(func() {
		currentPool := ants.SwapDefaultAntsPool(previousPool)
		if currentPool != nil && currentPool != previousPool {
			currentPool.Release()
		}
	})
	return sentinelPool
}

// assertDefaultPoolIdentity 使用临时 probe 获取默认池身份, 随后立即恢复原值.
func assertDefaultPoolIdentity(t *testing.T, want ants.Pooler) {
	t.Helper()
	probePool, err := ants.NewPool(1)
	if err != nil {
		t.Fatalf("create default pool probe: %v", err)
	}
	got := ants.SwapDefaultAntsPool(probePool)
	if got == nil {
		probePool.Release()
		t.Fatal("read default pool identity")
	}
	displacedProbe := ants.SwapDefaultAntsPool(got)
	if displacedProbe != probePool {
		probePool.Release()
		t.Fatal("restore default pool after identity probe")
	}
	probePool.Release()
	if got != want {
		t.Fatal("default pool identity was not restored")
	}
}
