package config

import (
	"os"
	"testing"
)

// TestTesterLifecycle 验证配置助手可重复执行并完整恢复调用前状态.
func TestTesterLifecycle(t *testing.T) {
	const envName = "PKG_CONFIG_TEST_RESTORE"
	oldEnv, hadEnv := os.LookupEnv(envName)
	t.Cleanup(func() {
		StopTester()
		if hadEnv {
			_ = os.Setenv(envName, oldEnv)
		} else {
			_ = os.Unsetenv(envName)
		}
	})

	oldConfig := Config()
	oldRootPath := RootPath
	oldConfigFile := ConfigFile
	oldInitialized := ConfigInitialized
	if err := os.Setenv(envName, "before"); err != nil {
		t.Fatalf("set test environment: %v", err)
	}

	for range 2 {
		InitTester()
		if Config() == nil || !ConfigInitialized {
			t.Fatal("tester did not initialize config")
		}
		if RootPath == oldRootPath || ConfigFile == oldConfigFile {
			t.Fatal("tester did not isolate config paths")
		}
		if err := os.Setenv(envName, "during"); err != nil {
			t.Fatalf("mutate test environment: %v", err)
		}

		StopTester()
		if Config() != oldConfig {
			t.Fatal("tester did not restore main config pointer")
		}
		if RootPath != oldRootPath || ConfigFile != oldConfigFile || ConfigInitialized != oldInitialized {
			t.Fatal("tester did not restore config globals")
		}
		if got := os.Getenv(envName); got != "before" {
			t.Fatalf("restored environment = %q, want %q", got, "before")
		}
	}
}

// TestTesterRejectsNestedInit 冻结助手不支持嵌套调用的边界.
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
