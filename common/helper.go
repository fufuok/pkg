package common

import (
	"net"
)

// LookupIPNetsString 从 IP 段集合中查询并返回对应数值
func LookupIPNetsString(s string, ipNets map[*net.IPNet]int64) (int64, bool) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, false
	}

	return LookupIPNets(ip, ipNets)
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
