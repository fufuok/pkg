package config

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/json"
)

// TestParseNodeInfoFileFormats 验证真实 node_info 字段顺序和项目扩展字段不影响通用节点信息.
func TestParseNodeInfoFileFormats(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "TOS node", body: `{"hostname":"spoofed","host_ip":"192.0.2.1","node_id":11067,"node_name":"北京接入-11067(test)","node_desc":"腾讯云北京-环网","node_type":"xunyou","service_ip":"36.110.143.182","inter_node_code":8,"tos_node":1}`},
		{name: "non-TOS node", body: `{"node_id":12691,"node_name":"东南亚接入-12691","node_desc":"腾讯云新加坡-环网","node_type":"xunyou","service_ip":"43.134.41.156","inter_node_code":4,"tos_node":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareConfigBehaviorTest(t)
			file := filepath.Join(t.TempDir(), "node_info.json")
			assert.Nil(t, os.WriteFile(file, []byte(tt.body), 0o600))
			cfg := &MainConf{NodeConf: NodeConf{
				NodeInfoFile: file,
				NodeInfo: NodeInfo{
					Hostname: "runtime-host",
					HostIP:   "10.0.0.1",
				},
			}}

			assert.Nil(t, parseNodeInfoJSON(cfg))
			assert.Equal(t, "xunyou", cfg.NodeConf.NodeInfo.NodeType)
			assert.True(t, cfg.NodeConf.NodeInfo.NodeID > 0)
			assert.Equal(t, "runtime-host", cfg.NodeConf.NodeInfo.Hostname)
			assert.Equal(t, "10.0.0.1", cfg.NodeConf.NodeInfo.HostIP)
			assert.Equal(t, file, NodeInfoFile)
		})
	}
}

// TestParseNodeInfoRejectsInvalidNodeType 验证类型错误不会把临时解析出的部分字段发布到配置.
func TestParseNodeInfoRejectsInvalidNodeType(t *testing.T) {
	prepareConfigBehaviorTest(t)
	file := filepath.Join(t.TempDir(), "node_info.json")
	assert.Nil(t, os.WriteFile(file, []byte(`{"node_id":99,"node_name":"partial","node_type":8,"service_ip":"127.0.0.99"}`), 0o600))
	want := NodeInfo{Hostname: "runtime-host", HostIP: "10.0.0.1", NodeID: 7, NodeIP: "127.0.0.7", NodeName: "stable", NodeType: "xunyou"}
	cfg := &MainConf{NodeConf: NodeConf{NodeInfoFile: file, NodeInfo: want}}

	err := parseNodeInfoJSON(cfg)
	assert.True(t, err != nil)
	assert.Equal(t, want, cfg.NodeConf.NodeInfo)
}

// TestParseNodeInfoBackupContract 验证 backup 默认关闭, 显式启用后才读取并回写有效节点信息.
func TestParseNodeInfoBackupContract(t *testing.T) {
	prepareConfigBehaviorTest(t)
	backupFile := filepath.Join(t.TempDir(), "node_info.backup")
	NodeInfoBackupFile = backupFile
	want := NodeInfo{Hostname: "saved-host", HostIP: "10.0.0.1", NodeID: 11, NodeIP: "127.0.0.11", NodeName: "saved", NodeType: "xunyou"}
	assert.Nil(t, os.WriteFile(backupFile, json.MustJSON(want), 0o600))

	disabled := &MainConf{}
	parseNodeInfoConfig(disabled)
	assert.Equal(t, net.IPv4zero.String(), disabled.NodeConf.NodeInfo.NodeIP)

	t.Setenv(NodeInfoBackupEnabledEnvName, "true")
	enabled := &MainConf{}
	parseNodeInfoConfig(enabled)
	assert.Equal(t, want.NodeID, enabled.NodeConf.NodeInfo.NodeID)
	assert.Equal(t, want.NodeIP, enabled.NodeConf.NodeInfo.NodeIP)
	assert.Equal(t, want.NodeName, enabled.NodeConf.NodeInfo.NodeName)

	var persisted NodeInfo
	body, err := os.ReadFile(backupFile)
	assert.Nil(t, err)
	assert.Nil(t, json.Unmarshal(body, &persisted))
	assert.Equal(t, want.NodeIP, persisted.NodeIP)
	assert.Equal(t, want.NodeType, persisted.NodeType)
}

// TestParseNodeInfoBackupRejectsInvalidNodeType 验证 backup 同样严格遵循字符串节点类型契约.
func TestParseNodeInfoBackupRejectsInvalidNodeType(t *testing.T) {
	prepareConfigBehaviorTest(t)
	NodeInfoBackupFile = filepath.Join(t.TempDir(), "node_info.backup")
	assert.Nil(t, os.WriteFile(NodeInfoBackupFile, []byte(`{"node_id":99,"node_type":8,"service_ip":"127.0.0.99"}`), 0o600))
	want := NodeInfo{Hostname: "runtime-host", HostIP: "10.0.0.1", NodeID: 7, NodeIP: "127.0.0.7", NodeType: "xunyou"}
	cfg := &MainConf{NodeConf: NodeConf{NodeInfo: want}}

	err := parseNodeInfoJSONBackup(cfg)
	assert.True(t, err != nil)
	assert.Contains(t, filepath.Base(NodeInfoBackupFile), err.Error())
	assert.Equal(t, want, cfg.NodeConf.NodeInfo)
}

// TestGetNodeIPFromAPIsUsesFirstValidLocalResult 验证多个本地 API 中只接受合法 IP 响应.
func TestGetNodeIPFromAPIsUsesFirstValidLocalResult(t *testing.T) {
	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	t.Cleanup(invalid.Close)
	valid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(" 203.0.113.9 \n"))
	}))
	t.Cleanup(valid.Close)

	got := GetNodeIPFromAPIs(invalid.URL+","+valid.URL, time.Second)
	assert.Equal(t, "203.0.113.9", got)
}

// TestParseNodeInfoUsesLocalIPAPI 验证本地 node_info 缺失时使用受控 HTTP API 填充服务 IP.
func TestParseNodeInfoUsesLocalIPAPI(t *testing.T) {
	prepareConfigBehaviorTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("198.51.100.8"))
	}))
	t.Cleanup(server.Close)
	cfg := &MainConf{NodeConf: NodeConf{IPAPI: server.URL}}

	parseNodeInfoConfig(cfg)
	assert.Equal(t, "198.51.100.8", cfg.NodeConf.NodeInfo.NodeIP)
}

// TestNodeIPFetcherDoesNotMutatePublishedMainConf 验证出口 IP 回填不会改已发布配置指针.
// NodeAgent / DataRouter / DataPlugins 热更新后 Config() 仍可能被其他 goroutine 读取.
func TestNodeIPFetcherDoesNotMutatePublishedMainConf(t *testing.T) {
	prepareConfigBehaviorTest(t)
	oldSleep := nodeIPFetcherSleep
	oldTimeout := ReqTimeoutDuration
	t.Cleanup(func() {
		nodeIPFetcherSleep = oldSleep
		ReqTimeoutDuration = oldTimeout
	})
	nodeIPFetcherSleep = func(time.Duration) {}
	ReqTimeoutDuration = time.Second

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.10"))
	}))
	t.Cleanup(server.Close)

	published := &MainConf{
		SYSConf: SYSConf{DebVersion: "published"},
		NodeConf: NodeConf{
			IPAPI:    server.URL,
			NodeInfo: NodeInfo{NodeIP: net.IPv4zero.String(), NodeName: "edge"},
		},
	}
	mainConf.Store(published)

	nodeIPFetcher(server.URL)

	assert.Equal(t, net.IPv4zero.String(), published.NodeConf.NodeInfo.NodeIP)
	assert.Equal(t, "edge", published.NodeConf.NodeInfo.NodeName)
	assert.Equal(t, "published", published.SYSConf.DebVersion)
	got := Config()
	assert.True(t, got != published)
	assert.Equal(t, "203.0.113.10", got.NodeConf.NodeInfo.NodeIP)
	assert.Equal(t, "edge", got.NodeConf.NodeInfo.NodeName)
	assert.Equal(t, "published", got.SYSConf.DebVersion)
	assert.Equal(t, "203.0.113.10", NodeIPFromAPI)
}

// TestSaveNodeInfoBackupSkipsZeroIP 验证无效节点不会覆盖已有 backup.
func TestSaveNodeInfoBackupSkipsZeroIP(t *testing.T) {
	prepareConfigBehaviorTest(t)
	NodeInfoBackupFile = filepath.Join(t.TempDir(), "node_info.backup")
	assert.Nil(t, os.WriteFile(NodeInfoBackupFile, []byte("original"), 0o600))

	saveNodeInfoBackup(NodeInfo{NodeIP: net.IPv4zero.String()})
	body, err := os.ReadFile(NodeInfoBackupFile)
	assert.Nil(t, err)
	assert.Equal(t, "original", string(body))
}
