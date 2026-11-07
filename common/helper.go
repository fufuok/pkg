package common

import (
	"net"
	"net/netip"
)

// LookupIPNetsString 从 IP 段集合中查询并返回对应数值.
// 使用 netip.ParseAddr 在栈上完成解析, 避免 net.ParseIP 每次请求分配堆内存; 拒绝带 zone 的地址以保持与 net.ParseIP 一致.
func LookupIPNetsString(s string, ipNets map[*net.IPNet]int64) (int64, bool) {
	if len(ipNets) == 0 {
		return 0, false
	}
	addr, err := netip.ParseAddr(s)
	if err != nil || addr.Zone() != "" {
		return 0, false
	}
	raw := addr.As16()
	return LookupIPNets(raw[:], ipNets)
}

// LookupIPNets 从 IP 段集合中查询并返回对应数值
func LookupIPNets(ip net.IP, ipNets map[*net.IPNet]int64) (int64, bool) {
	for ipNet, val := range ipNets {
		if ipNet.Contains(ip) {
			return val, true
		}
	}
	return 0, false
}
