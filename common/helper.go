package common

import (
	"net"
)

// LookupIPNetsString 从 IP 段集合中查询并返回对应数值; 集合为空时不解析 IP 直接返回未命中.
// net.ParseIP 内部即 netip.ParseAddr + As16 且拒绝带 zone 的地址, 结果不逃逸时留在栈上, 无需手写 netip 版本.
func LookupIPNetsString(s string, ipNets map[*net.IPNet]int64) (int64, bool) {
	if len(ipNets) == 0 {
		return 0, false
	}
	return LookupIPNets(net.ParseIP(s), ipNets)
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
