package common

import (
	"net"
	"strconv"

	"github.com/fufuok/utils/xhash"
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
	tss := strconv.FormatInt(ts, 10)
	return GenSignString(tss, key)
}

// GenSignString 字符串类型的时间戳生成签名
func GenSignString(ts, key string) string {
	if len(ts) != 10 || key == "" {
		return ""
	}
	sign := xhash.MD5Hex(ts + key)
	return ts + sign
}

// GenSignNow 以当前时间时间戳生成签名
func GenSignNow(key string) (int64, string) {
	ts := GTimestamp()
	return ts, GenSign(ts, key)
}

// VerifySign 校验签名
func VerifySign(key, sign string) bool {
	if key == "" || len(sign) != 42 {
		return false
	}
	return sign == GenSignString(sign[:10], key)
}

// VerifySignTTL 校验签名及签名有效期(当前时间 **秒 范围内有效)
func VerifySignTTL(key, sign string, second int64) bool {
	if ok := VerifySign(key, sign); !ok {
		return false
	}
	ts, _ := strconv.ParseInt(sign[:10], 10, 64)
	now := GTimestamp()
	return ts >= now-second && ts <= now+second
}
