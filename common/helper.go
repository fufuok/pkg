package common

import (
	"net"

	"github.com/fufuok/pkg/xsign"
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

// GenSign 使用时间戳和密钥生成简单签名字符串
// 算法: md5(ts+key)
// 结果: ts+sign
func GenSign(ts int64, key string) string {
	return xsign.GenSign(ts, key)
}

// GenSignString 字符串类型的时间戳生成签名
func GenSignString(ts, key string) string {
	return xsign.GenSignString(ts, key)
}

// GenSignNow 以 NTP 校正后的当前时间戳生成签名
func GenSignNow(key string) (int64, string) {
	ts := GTimestamp()
	return ts, xsign.GenSign(ts, key)
}

// VerifySign 校验签名
func VerifySign(key, sign string) bool {
	return xsign.VerifySign(key, sign)
}

// VerifySignTTL 校验签名及签名有效期(当前时间 **秒 范围内有效)
// 时间源仍是 GTimestamp, 与迁入 xsign 前一致.
func VerifySignTTL(key, sign string, second int64) bool {
	return xsign.VerifySignTTLAt(key, sign, second, GTimestamp())
}
