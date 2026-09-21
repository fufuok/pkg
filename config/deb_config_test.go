package config

import (
	"testing"

	"github.com/fufuok/pkg/assert"
)

// TestParseSYSConfigPreservesDebianVersions 验证配置层只裁剪空白, 合法和非法版本均不会被静默改写.
func TestParseSYSConfigPreservesDebianVersions(t *testing.T) {
	prepareConfigBehaviorTest(t)
	for _, version := range []string{"1:1.2.3+build~rc1-2", "1.2.3;rm", "", "bad version"} {
		t.Run(version, func(t *testing.T) {
			cfg := &MainConf{SYSConf: SYSConf{DebVersion: " \t" + version + "\n"}}
			assert.Nil(t, parseSYSConfig(cfg))
			assert.Equal(t, version, cfg.SYSConf.DebVersion)
		})
	}
}

// TestPublishNodeIPPreservesReloadedSnapshot 用 channel 固定旧 IP 发布与主配置更新的交错顺序.
// 旧候选必须 CAS 失败并合并新快照, 且不得把已撤销的灰度写回.
func TestPublishNodeIPPreservesReloadedSnapshot(t *testing.T) {
	prepareConfigBehaviorTest(t)
	old := &MainConf{
		SYSConf: SYSConf{DebVersion: "1.0", CanaryDeployment: 100},
	}
	latest := &MainConf{
		SYSConf: SYSConf{DebVersion: "2.0", CanaryDeployment: 0},
	}
	mainConf.Store(old)
	observed := make(chan struct{})
	publish := make(chan struct{})
	done := make(chan struct{})
	go func() {
		candidate := mainConf.Load()
		close(observed)
		<-publish
		publishNodeIP(candidate, "203.0.113.10")
		close(done)
	}()
	<-observed
	mainConf.Store(latest)
	close(publish)
	<-done

	got := Config()
	assert.Equal(t, latest.SYSConf, got.SYSConf)
	assert.Equal(t, "203.0.113.10", got.NodeConf.NodeInfo.NodeIP)
	assert.Equal(t, "", old.NodeConf.NodeInfo.NodeIP)
	assert.Equal(t, "", latest.NodeConf.NodeInfo.NodeIP)
	assert.True(t, got != latest)
}

// TestPublishNodeIPKeepsExplicitAddress 验证重载已获得显式 IP 时, 晚到的 API 响应只丢弃而不覆盖.
func TestPublishNodeIPKeepsExplicitAddress(t *testing.T) {
	prepareConfigBehaviorTest(t)
	old := &MainConf{SYSConf: SYSConf{DebVersion: "1.0"}}
	latest := &MainConf{
		SYSConf:  SYSConf{DebVersion: "2.0"},
		NodeConf: NodeConf{NodeInfo: NodeInfo{NodeIP: "198.51.100.7"}},
	}
	mainConf.Store(latest)
	publishNodeIP(old, "203.0.113.10")
	assert.True(t, Config() == latest)
	assert.Equal(t, "198.51.100.7", Config().NodeConf.NodeInfo.NodeIP)
}
