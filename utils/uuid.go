package utils

import (
	"encoding/hex"
	"uuid"

	"github.com/fufuok/pkg/base58"
)

// UUIDString 返回随机 UUID v4 的小写十六进制文本, 使用 8-4-4-4-12 短横线格式.
func UUIDString() string {
	return B2S(EncodeUUID(UUID()))
}

// UUIDSimple 返回随机 UUID v4 的 32 位小写十六进制文本, 不含短横线.
func UUIDSimple() string {
	return hex.EncodeToString(UUID())
}

// UUIDShort 返回随机 UUID v4 的 base58 文本, 保留全部 16 字节, 编码长度不固定.
func UUIDShort() string {
	return base58.Encode(UUID())
}

// UUID 返回独立可写的 16 字节随机 UUID v4, 符合 RFC 9562.
// 标准库使用密码学随机源生成 122 位随机数据; 多次调用及重启后的结果不可复现.
func UUID() []byte {
	id := uuid.NewV4()
	return id[:]
}

// EncodeUUID 返回 36 字节的小写十六进制短横线文本, 不修改输入或验证版本及变体.
// 输入不足 16 字节时在末尾补零, 超出时只取前 16 字节; nil 与空切片编码为全零 UUID.
// 相同输入始终得到相同结果, 不受进程重启影响.
func EncodeUUID(id []byte) []byte {
	src := make([]byte, 16)
	copy(src, id)
	dst := make([]byte, 36)
	hex.Encode(dst, src[:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], src[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], src[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], src[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:], src[10:])

	return dst
}
