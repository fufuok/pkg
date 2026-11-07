package common

import (
	"net"
	"testing"
)

func mustParseCIDRMap(tb testing.TB, entries map[string]int64) map[*net.IPNet]int64 {
	tb.Helper()
	out := make(map[*net.IPNet]int64, len(entries))
	for cidr, val := range entries {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			tb.Fatalf("parse CIDR %q: %v", cidr, err)
		}
		out[ipNet] = val
	}
	return out
}

// TestLookupIPNetsStringEquivalenceAndZeroAlloc 验证零分配解析与标准库 net.ParseIP 语义一致.
func TestLookupIPNetsStringEquivalenceAndZeroAlloc(t *testing.T) {
	ipNets := mustParseCIDRMap(t, map[string]int64{
		"10.0.0.0/8":            10,
		"192.168.1.50/32":       50,
		"2001:db8::/32":         60,
		"::ffff:172.16.1.9/128": 70,
	})

	cases := []struct {
		ip      string
		wantVal int64
		wantOK  bool
	}{
		{ip: "10.1.2.3", wantVal: 10, wantOK: true},
		{ip: "192.168.1.50", wantVal: 50, wantOK: true},
		{ip: "::ffff:192.168.1.50", wantVal: 50, wantOK: true},
		{ip: "172.16.1.9", wantVal: 70, wantOK: true},
		{ip: "2001:db8::abcd", wantVal: 60, wantOK: true},
		{ip: "203.0.113.9", wantVal: 0, wantOK: false},
		{ip: "fe80::1%eth0", wantVal: 0, wantOK: false},
		{ip: "", wantVal: 0, wantOK: false},
		{ip: "not-an-ip", wantVal: 0, wantOK: false},
	}

	for _, tc := range cases {
		gotVal, gotOK := LookupIPNetsString(tc.ip, ipNets)
		if gotVal != tc.wantVal || gotOK != tc.wantOK {
			t.Fatalf("LookupIPNetsString(%q) = (%d, %v), want (%d, %v)",
				tc.ip, gotVal, gotOK, tc.wantVal, tc.wantOK)
		}
	}

	if v, ok := LookupIPNetsString("10.0.0.1", nil); v != 0 || ok {
		t.Fatalf("empty ipNets lookup = (%d, %v), want (0, false)", v, ok)
	}

	allocs := testing.AllocsPerRun(100, func() {
		_, _ = LookupIPNetsString("192.168.1.50", ipNets)
		_, _ = LookupIPNetsString("2001:db8::1", ipNets)
		_, _ = LookupIPNetsString("203.0.113.9", ipNets)
	})
	if allocs != 0 {
		t.Fatalf("LookupIPNetsString allocs = %v, want 0", allocs)
	}
}

func BenchmarkLookupIPNetsString(b *testing.B) {
	ipNets := mustParseCIDRMap(b, map[string]int64{
		"10.0.0.0/8":     1,
		"172.16.0.0/12":  2,
		"192.168.0.0/16": 3,
		"2001:db8::/32":  4,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = LookupIPNetsString("203.0.113.42", ipNets)
	}
}
