package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fufuok/utils/assert"
)

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
