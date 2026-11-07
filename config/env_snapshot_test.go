package config

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestLoadEnvFilesUsesSingleSnapshot 验证验证后的文件替换不会改变已受理的内容或 key 集合.
func TestLoadEnvFilesUsesSingleSnapshot(t *testing.T) {
	prepareConfigBehaviorTest(t)
	for _, key := range []string{"PKG_ENV_SNAPSHOT", "PKG_ENV_UNCHECKED"} {
		clearTestEnvironment(t, key)
	}
	if err := os.WriteFile(EnvMainFile, []byte("PKG_ENV_SNAPSHOT=accepted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRead := readEnvFile
	t.Cleanup(func() { readEnvFile = oldRead })
	reads := 0
	readEnvFile = func(path string) ([]byte, error) {
		reads++
		body, err := os.ReadFile(path)
		if err == nil {
			err = os.WriteFile(path, []byte("PKG_ENV_SNAPSHOT=changed\nPKG_ENV_UNCHECKED=unexpected\n"), 0o600)
		}
		return body, err
	}

	if err := loadEnvFiles("missing.env"); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || os.Getenv("PKG_ENV_SNAPSHOT") != "accepted" || os.Getenv("PKG_ENV_UNCHECKED") != "" {
		t.Fatalf("load did not use exactly one content snapshot per file: reads=%d", reads)
	}
	if !maps.Equal(envFileKeys, map[string]struct{}{"PKG_ENV_SNAPSHOT": {}}) {
		t.Fatal("tracked keys do not match the applied snapshot")
	}
	if !slices.Equal(extraEnvFiles, []string{EnvMainFile, filepath.Join(EnvFilePath, "missing.env")}) {
		t.Fatal("missing files were removed from the tracked file list")
	}
}

// TestLoadEnvFilesExpansionScope 验证展开保留 godotenv 的文件内顺序语义, 不读取其他文件或进程值.
func TestLoadEnvFilesExpansionScope(t *testing.T) {
	prepareConfigBehaviorTest(t)
	for _, key := range []string{"PKG_ENV_SOURCE", "PKG_ENV_LOCAL", "PKG_ENV_EXTERNAL", "PKG_ENV_LITERAL"} {
		t.Setenv(key, "process")
	}
	if err := os.WriteFile(EnvMainFile, []byte("PKG_ENV_SOURCE=first\nPKG_ENV_LOCAL=${PKG_ENV_SOURCE}/local\nPKG_ENV_LITERAL='$PKG_ENV_SOURCE'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(EnvFilePath, "extra.env")
	if err := os.WriteFile(extra, []byte("PKG_ENV_EXTERNAL=${PKG_ENV_SOURCE}/external\nPKG_ENV_SOURCE=second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFiles(extra); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"PKG_ENV_SOURCE": "second", "PKG_ENV_LOCAL": "first/local",
		"PKG_ENV_EXTERNAL": "/external", "PKG_ENV_LITERAL": "$PKG_ENV_SOURCE",
	} {
		if got := os.Getenv(key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

// TestLoadEnvFilesReadFailurePreservesState 验证读取错误保留根因和上轮跟踪状态.
func TestLoadEnvFilesReadFailurePreservesState(t *testing.T) {
	prepareConfigBehaviorTest(t)
	t.Setenv("PKG_ENV_EXISTING", "old")
	envFileKeys = map[string]struct{}{"PKG_ENV_EXISTING": {}}
	extraEnvFiles = []string{"previous.env"}
	oldRead := readEnvFile
	t.Cleanup(func() { readEnvFile = oldRead })
	want := errors.New("fixture read failure")
	readEnvFile = func(string) ([]byte, error) { return nil, want }
	err := loadEnvFiles()
	if !errors.Is(err, want) || !strings.Contains(err.Error(), filepath.Base(EnvMainFile)) {
		t.Fatalf("read error lost its cause or file context: %v", err)
	}
	if os.Getenv("PKG_ENV_EXISTING") != "old" || !slices.Equal(extraEnvFiles, []string{"previous.env"}) || len(envFileKeys) != 1 {
		t.Fatal("read failure changed tracked state")
	}
}

// TestLoadEnvFilesRejectsInvalidPreviousKey 验证旧版本留下的不可写 key 不再被静默忽略.
func TestLoadEnvFilesRejectsInvalidPreviousKey(t *testing.T) {
	prepareConfigBehaviorTest(t)
	envFileKeys = map[string]struct{}{"": {}}
	extraEnvFiles = []string{"previous.env"}
	err := loadEnvFiles()
	if err == nil || !strings.Contains(err.Error(), "clear removed env key") {
		t.Fatalf("invalid tracked key did not report a clear error: %v", err)
	}
	if !slices.Equal(extraEnvFiles, []string{"previous.env"}) || len(envFileKeys) != 1 {
		t.Fatal("clear failure published a new key snapshot")
	}
}

// TestLoadEnvFilesRejectsInvalidEnvironment 验证不可写的键值在应用任何文件前被拒绝.
// 旧实现会忽略 Setenv 的错误并发布与进程环境不一致的 key 集合.
func TestLoadEnvFilesRejectsInvalidEnvironment(t *testing.T) {
	for name, body := range map[string]string{"empty_key": "=private-value\n", "nul_value": "PKG_ENV_BAD=private\x00value\n"} {
		t.Run(name, func(t *testing.T) {
			prepareConfigBehaviorTest(t)
			t.Setenv("PKG_ENV_EXISTING", "old")
			envFileKeys = map[string]struct{}{"PKG_ENV_EXISTING": {}}
			extraEnvFiles = []string{"previous.env"}
			beforeKeys, beforeFiles := maps.Clone(envFileKeys), slices.Clone(extraEnvFiles)
			if err := os.WriteFile(EnvMainFile, []byte("PKG_ENV_EXISTING=new\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			extra := filepath.Join(EnvFilePath, "invalid.env")
			if err := os.WriteFile(extra, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			err := loadEnvFiles(extra)
			if err == nil {
				t.Fatal("invalid environment entry was accepted")
			}
			if !strings.Contains(err.Error(), filepath.Base(extra)) || strings.Contains(err.Error(), "private") {
				t.Fatalf("error must identify the file without exposing its value: %v", err)
			}
			if os.Getenv("PKG_ENV_EXISTING") != "old" || !maps.Equal(beforeKeys, envFileKeys) || !slices.Equal(beforeFiles, extraEnvFiles) {
				t.Fatal("failed validation changed the environment or tracked state")
			}
		})
	}
}

// TestLoadEnvFilesParseErrorKeepsValuesPrivate 验证解析失败不回显可能含密钥的源文本.
func TestLoadEnvFilesParseErrorKeepsValuesPrivate(t *testing.T) {
	prepareConfigBehaviorTest(t)
	t.Setenv("PKG_ENV_EXISTING", "old")
	if err := os.WriteFile(EnvMainFile, []byte("PKG_ENV_EXISTING=new\nPKG_ENV_SECRET=\"private-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := loadEnvFiles()
	if err == nil || !strings.Contains(err.Error(), filepath.Base(EnvMainFile)) {
		t.Fatalf("expected parse error with file context, got %v", err)
	}
	if strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("parse error exposed a configuration value: %v", err)
	}
	if os.Getenv("PKG_ENV_EXISTING") != "old" {
		t.Fatal("failed parsing changed the environment")
	}
}
