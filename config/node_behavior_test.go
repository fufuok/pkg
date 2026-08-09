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

// TestParseNodeInfoFileFormats 验证新旧 node_info 文件的节点类型和字段映射.
func TestParseNodeInfoFileFormats(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType int
	}{
		{name: "new node type", body: `{"node_id":7,"service_ip":"127.0.0.7","node_name":"new","inter_node_type":"edge","inter_node_code":9}`, wantType: 9},
		{name: "legacy normal node", body: `{"node_id":8,"service_ip":"127.0.0.8","node_name":"legacy","inter_node":"普通节点"}`, wantType: 0},
		{name: "legacy overseas node", body: `{"node_id":9,"service_ip":"127.0.0.9","inter_node":"海外版_海外用户接入"}`, wantType: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prepareConfigBehaviorTest(t)
			file := filepath.Join(t.TempDir(), "node_info.json")
			assert.Nil(t, os.WriteFile(file, []byte(tt.body), 0o600))
			cfg := &MainConf{NodeConf: NodeConf{NodeInfoFile: file}}

			parseNodeInfoJson(cfg)
			assert.Equal(t, tt.wantType, cfg.NodeConf.NodeInfo.NodeType)
			assert.True(t, cfg.NodeConf.NodeInfo.NodeID > 0)
			assert.Equal(t, file, NodeInfoFile)
		})
	}
}

// TestParseNodeInfoBackupContract 验证 backup 默认关闭, 显式启用后才读取并回写有效节点信息.
func TestParseNodeInfoBackupContract(t *testing.T) {
	prepareConfigBehaviorTest(t)
	backupFile := filepath.Join(t.TempDir(), "node_info.backup")
	NodeInfoBackupFile = backupFile
	want := NodeInfo{Hostname: "saved-host", HostIP: "10.0.0.1", NodeID: 11, NodeIP: "127.0.0.11", NodeName: "saved", NodeType: 2}
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
