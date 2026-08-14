package xcrypto

import (
	"os"

	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
)

// SetenvEncrypt 用 Encrypt 加密 value 后写入环境变量 key.
// 返回值是写入后的密文; 仅 os.Setenv 失败时返回错误, 加密失败会被吞成空串或原文.
func SetenvEncrypt(key, value, secret string) (string, error) {
	value = Encrypt(value, secret)
	if err := os.Setenv(key, value); err != nil {
		return "", err
	}

	return value, nil
}

// GetenvDecrypt 读取环境变量 key, 再用 Decrypt 还原.
// 缺失变量、错误密钥或损坏密文通常得到空串, 由调用方判断是否拒绝启动.
func GetenvDecrypt(key string, secret string) string {
	return Decrypt(os.Getenv(key), secret)
}

// Encrypt 是确定性配置包装, 不是通用加密, 也不是 AEAD.
// 算法: secret 做 MD5Hex 得到 32 字节 AES-256 密钥, AES-CBC + Zeros padding,
// 默认 IV 为 key[:16], 输出 base58. secret 为空时原样返回明文.
// 同一明文+同一密钥永远得到同一密文; 错误被吞掉, 失败时可能返回空串.
// 已有 BASE_SECRET_KEY 和业务密钥依赖该输出, 不能改算法或编码.
func Encrypt(value, secret string) string {
	if secret != "" {
		value = AesCBCEnStringB58(value, utils.S2B(xhash.MD5Hex(secret)))
	}

	return value
}

// Decrypt 是 Encrypt 的逆过程, 算法和边界与 Encrypt 相同.
// 密文被改或密钥错误时不会明确报错, 可能得到空串或不可用明文.
func Decrypt(value, secret string) string {
	if secret != "" {
		value = AesCBCDeStringB58(value, utils.S2B(xhash.MD5Hex(secret)))
	}

	return value
}
