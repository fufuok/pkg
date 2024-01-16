package common

import (
	"net"
)

// LookupIPNets 从 IP 段集合中查询并返回对应数值
func LookupIPNets(s string, ipNets map[*net.IPNet]int64) (int64, bool) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, false
	}

	for ipNet, val := range ipNets {
		if ipNet.Contains(ip) {
			return val, true
		}
	}
	return 0, false
}
