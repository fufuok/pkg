package xhash

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"

	"github.com/fufuok/pkg/utils"
)

// HmacSHA256Hex 返回稳定的 HMAC-SHA-256 小写十六进制值.
// 相同消息和密钥在重启后和跨进程仍相同, 不能当作无密钥校验和.
// 与相同内容的 HmacSHA256 原始结果经十六进制编码后相同, 验证原始 MAC 时应使用 hmac.Equal.
func HmacSHA256Hex(s, key string) string {
	return hex.EncodeToString(HmacSHA256(utils.S2B(s), utils.S2B(key)))
}

// HmacSHA256 返回稳定的 HMAC-SHA-256 原始值.
// 相同消息和密钥在重启后和跨进程仍相同, key 应按密钥保护.
func HmacSHA256(b, key []byte) []byte {
	return Hmac(b, key, sha256.New)
}

// HmacSHA512Hex 返回稳定的 HMAC-SHA-512 小写十六进制值.
// 相同消息和密钥在重启后和跨进程仍相同.
// 需要原始字节时使用 HmacSHA512, 不能与 HmacSHA256Hex 的输出直接互换.
func HmacSHA512Hex(s, key string) string {
	return hex.EncodeToString(HmacSHA512(utils.S2B(s), utils.S2B(key)))
}

// HmacSHA512 返回稳定的 HMAC-SHA-512 原始值.
// 相同消息和密钥在重启后和跨进程仍相同. 适用于带密钥的完整性校验, 不适合进程内高频 Map.
func HmacSHA512(b, key []byte) []byte {
	return Hmac(b, key, sha512.New)
}

// HmacSHA1Hex 返回稳定的 HMAC-SHA-1 小写十六进制值.
// 相同消息和密钥在重启后和跨进程仍相同. 可用于兼容既有校验器, 新代码应使用 HMAC-SHA-256.
// 对相同消息和密钥, 它是 HmacSHA1 原始结果的小写十六进制表示.
func HmacSHA1Hex(s, key string) string {
	return hex.EncodeToString(HmacSHA1(utils.S2B(s), utils.S2B(key)))
}

// HmacSHA1 返回稳定的 HMAC-SHA-1 原始值.
// 相同消息和密钥在重启后和跨进程仍相同, 但 SHA-1 不建议用于新协议.
func HmacSHA1(b, key []byte) []byte {
	return Hmac(b, key, sha1.New)
}
