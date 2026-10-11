//go:build linux

package stats

import "testing"

// TestParseSystemFileHandles 验证 file-nr 的使用量计算和格式错误分支.
func TestParseSystemFileHandles(t *testing.T) {
	used, limit, err := parseSystemFileHandles("100 7 200\n")
	if err != nil || used != 93 || limit != 200 {
		t.Fatalf("file-nr = used:%d limit:%d err:%v", used, limit, err)
	}
	if _, _, err := parseSystemFileHandles("100 200 300"); err == nil {
		t.Fatal("expected invalid file-nr counters error")
	}
}

// TestParseTcpExtLine 只冻结看板使用的两项 TcpExt 字段, 忽略内核新增字段.
func TestParseTcpExtLine(t *testing.T) {
	header := "TcpExt: ListenOverflows SyncookiesSent ListenDrops"
	values := "TcpExt: 11 22 33"
	got, err := parseTcpExtLine(header, values)
	if err != nil || got["ListenOverflows"] != 11 || got["ListenDrops"] != 33 {
		t.Fatalf("TcpExt = %#v err:%v", got, err)
	}
	if _, err := parseTcpExtLine(header, "TcpExt: 11"); err == nil {
		t.Fatal("expected invalid TcpExt format error")
	}
}
