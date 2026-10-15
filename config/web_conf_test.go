package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/xcrypto"
)

// TestWebConfIPValidation 验证可选开关的 JSON 入口、显式开启与分组继承语义.
// 布尔 false 沿用现有零值继承规则, 不用于关闭主配置已经开启的选项.
func TestWebConfIPValidation(t *testing.T) {
	var cfg WebConf
	if err := json.Unmarshal([]byte(`{"enable_ip_validation":true}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if !NormalizeWebConf(WebConf{}, cfg).EnableIPValidation {
		t.Fatal("explicit IP validation was not preserved")
	}
	if NormalizeWebConf(WebConf{}, WebConf{}).EnableIPValidation {
		t.Fatal("IP validation must remain disabled by default")
	}
	if !NormalizeWebGroupConf(cfg, "api", WebConf{}).EnableIPValidation {
		t.Fatal("group must inherit enabled IP validation")
	}
}

// TestWebConfBodyLimit 验证加载、覆盖和分组入口统一使用正数或 pkg 默认上限.
// 负数不再保留不存在的无限语义; 分组零值仍可继承主配置的明确上限.
func TestWebConfBodyLimit(t *testing.T) {
	prepareConfigBehaviorTest(t)
	for _, limit := range []int{-2, -1, 0, 1024} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			want := limit
			if want <= 0 {
				want = WebBodyLimit
			}
			cfg := &MainConf{WebConf: WebConf{BodyLimit: limit}}
			parseWebConfig(cfg)
			if cfg.WebConf.BodyLimit != want {
				t.Fatalf("loaded BodyLimit = %d, want %d", cfg.WebConf.BodyLimit, want)
			}
			if got := NormalizeWebConf(WebConf{}, WebConf{BodyLimit: limit}); got.BodyLimit != want {
				t.Fatalf("normalized BodyLimit = %d, want %d", got.BodyLimit, want)
			}
			if got := NormalizeWebGroupConf(WebConf{}, "api", WebConf{BodyLimit: limit}); got.BodyLimit != want {
				t.Fatalf("group BodyLimit = %d, want %d", got.BodyLimit, want)
			}
		})
	}
	base := WebConf{BodyLimit: 16 << 20}
	if got := NormalizeWebGroupConf(base, "api", WebConf{}); got.BodyLimit != base.BodyLimit {
		t.Fatal("group zero value must inherit the base limit")
	}
}

// TestResolveGroupCertFile 覆盖分组独立 TLS 证书解析的三种边界:
// 未指定证书环境变量(继承主配置), 指定且文件有效(使用独立证书), 指定但文件无效(清空, 不回退主证书).
func TestResolveGroupCertFile(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "group.crt")
	keyFile := filepath.Join(dir, "group.key")
	assert.Nil(t, os.WriteFile(certFile, []byte("cert"), 0o600))
	assert.Nil(t, os.WriteFile(keyFile, []byte("key"), 0o600))

	// 用例 1: 未指定证书环境变量, 保持继承的主配置证书不变
	groupCfg := WebConf{CertFile: "main.crt", KeyFile: "main.key"}
	resolveGroupCertFile(&groupCfg, "", "")
	assert.Equal(t, "main.crt", groupCfg.CertFile)
	assert.Equal(t, "main.key", groupCfg.KeyFile)

	// 用例 2: 指定证书环境变量且文件有效, 使用分组独立证书覆盖主配置
	t.Setenv("TEST_GROUP_CERT", certFile)
	t.Setenv("TEST_GROUP_KEY", keyFile)
	groupCfg = WebConf{CertFile: "main.crt", KeyFile: "main.key"}
	resolveGroupCertFile(&groupCfg, "TEST_GROUP_CERT", "TEST_GROUP_KEY")
	assert.Equal(t, certFile, groupCfg.CertFile)
	assert.Equal(t, keyFile, groupCfg.KeyFile)

	// 用例 3: 指定证书环境变量但文件无效, 清空证书(不回退主配置), 由调用方据此关闭 HTTPS
	t.Setenv("TEST_GROUP_CERT", filepath.Join(dir, "missing.crt"))
	t.Setenv("TEST_GROUP_KEY", filepath.Join(dir, "missing.key"))
	groupCfg = WebConf{CertFile: "main.crt", KeyFile: "main.key"}
	resolveGroupCertFile(&groupCfg, "TEST_GROUP_CERT", "TEST_GROUP_KEY")
	assert.Equal(t, "", groupCfg.CertFile)
	assert.Equal(t, "", groupCfg.KeyFile)
}

// TestNormalizeWebGroupConf 验证分组配置归一化: 继承主配置通用项, 但监听地址不继承主端口.
func TestNormalizeWebGroupConf(t *testing.T) {
	base := WebConf{
		ServerAddr:      ":80",
		ServerHttpsAddr: ":443",
		TrustedProxies:  []string{"10.0.0.0/8"},
		BodyLimit:       1024,
	}
	// 分组仅配置自身端口, 通用项应继承主配置, 端口不应继承主端口
	group := WebConf{ServerAddr: ":8080"}
	got := NormalizeWebGroupConf(base, "api", group)

	assert.Equal(t, ":8080", got.ServerAddr)
	assert.Equal(t, "", got.ServerHttpsAddr) // 未显式配置则为空, 不继承主 :443
	assert.Equal(t, "api", got.Name)         // Name 为空时取分组名
	assert.Equal(t, 1024, got.BodyLimit)     // 通用项继承
	assert.True(t, got.Groups == nil)        // 分组配置不再嵌套 Groups
	assert.Equal(t, 1, len(got.TrustedProxies))
}

// TestParseWebConfigGroups 验证主配置默认值、主证书和分组独立证书的完整派生规则.
func TestParseWebConfigGroups(t *testing.T) {
	prepareConfigBehaviorTest(t)
	dir := t.TempDir()
	mainCert := filepath.Join(dir, "main.crt")
	mainKey := filepath.Join(dir, "main.key")
	groupCert := filepath.Join(dir, "group.crt")
	groupKey := filepath.Join(dir, "group.key")
	for _, file := range []string{mainCert, mainKey, groupCert, groupKey} {
		assert.Nil(t, os.WriteFile(file, []byte("test"), 0o600))
	}
	t.Setenv(WebCertFileEnvName, mainCert)
	t.Setenv(WebKeyFileEnvName, mainKey)
	t.Setenv("PKG_GROUP_CERT", groupCert)
	t.Setenv("PKG_GROUP_KEY", groupKey)
	if _, err := xcrypto.SetenvEncrypt(WebSignKeyEnvName, "sign-key", "unit-test-secret"); err != nil {
		t.Fatalf("encrypt web sign key: %v", err)
	}

	cfg := &MainConf{
		SYSConf: SYSConf{BaseSecretValue: "unit-test-secret"},
		WebConf: WebConf{Groups: map[string]WebConf{
			"api":   {ServerAddr: ":18080"},
			"admin": {ServerHttpsAddr: ":18443", CertFileEnv: "PKG_GROUP_CERT", KeyFileEnv: "PKG_GROUP_KEY"},
		}},
	}
	parseWebConfig(cfg)

	assert.Equal(t, WebServerAddr, cfg.WebConf.ServerAddr)
	assert.Equal(t, WebServerHttpsAddr, cfg.WebConf.ServerHttpsAddr)
	assert.Equal(t, WebBodyLimit, cfg.WebConf.BodyLimit)
	assert.Equal(t, int64(WebSignTTLDefault), cfg.WebConf.SignTTL)
	assert.Equal(t, "sign-key", cfg.WebConf.SignKey)
	assert.Equal(t, ":18080", cfg.WebConf.Groups["api"].ServerAddr)
	assert.Equal(t, "", cfg.WebConf.Groups["api"].ServerHttpsAddr)
	assert.Equal(t, mainCert, cfg.WebConf.Groups["api"].CertFile)
	assert.Equal(t, ":18443", cfg.WebConf.Groups["admin"].ServerHttpsAddr)
	assert.Equal(t, groupCert, cfg.WebConf.Groups["admin"].CertFile)
	assert.Equal(t, groupKey, cfg.WebConf.Groups["admin"].KeyFile)
}
