// Package xsign 提供两类签名, 都不是加密, 也不能替换 xcrypto.Encrypt / Seal.
//
// GenSign 是 pkg 通用 ts+key MD5; MSGenSign 是微服务拼串 MD5.
package xsign

import (
	"strconv"
	"time"

	"github.com/fufuok/pkg/xhash"
)

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

// GenSignNow 以当前本地 Unix 秒生成签名.
// 需要 NTP 校正时间的调用方应继续走 common.GenSignNow.
func GenSignNow(key string) (int64, string) {
	ts := time.Now().Unix()
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
	return VerifySignTTLAt(key, sign, second, time.Now().Unix())
}

// VerifySignTTLAt 按给定 Unix 秒校验签名有效期.
// common.VerifySignTTL 传入 GTimestamp, 以保留 NTP 校正时间.
func VerifySignTTLAt(key, sign string, second, now int64) bool {
	if ok := VerifySign(key, sign); !ok {
		return false
	}
	ts, _ := strconv.ParseInt(sign[:10], 10, 64)
	return ts >= now-second && ts <= now+second
}
