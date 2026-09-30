package xhash

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"os"

	"github.com/fufuok/pkg/utils"
)

// Sha256Hex 返回稳定的 SHA-256 小写十六进制摘要.
// 相同输入在重启后和跨进程仍相同. 适用于内容校验和持久化标识, 不是带密钥的 MAC, 也不适合高频缓存.
func Sha256Hex(s string) string {
	return hex.EncodeToString(Sha256(utils.S2B(s)))
}

// Sha256 返回稳定的 SHA-256 原始摘要.
// 相同输入在重启后和跨进程仍相同, 可跨进程持久化.
func Sha256(b []byte) []byte {
	return Hash(b, sha256.New())
}

// Sha512Hex 返回稳定的 SHA-512 小写十六进制摘要.
// 相同输入在重启后和跨进程仍相同. 适用于内容完整性校验, 不适合缓存键或高频哈希.
func Sha512Hex(s string) string {
	return hex.EncodeToString(Sha512(utils.S2B(s)))
}

// Sha512 返回稳定的 SHA-512 原始摘要.
// 相同输入在重启后和跨进程仍相同, 与 Go 版本无关.
func Sha512(b []byte) []byte {
	return Hash(b, sha512.New())
}

// Sha1Hex 返回稳定的 SHA-1 小写十六进制摘要.
// 相同输入在重启后和跨进程仍相同. 可用于兼容既有协议, 新的完整性校验应使用 SHA-256.
func Sha1Hex(s string) string {
	return hex.EncodeToString(Sha1(utils.S2B(s)))
}

// Sha1 返回稳定的 SHA-1 原始摘要.
// 相同输入在重启后和跨进程仍相同, 但密码学强度不足, 不宜用于新设计.
func Sha1(b []byte) []byte {
	return Hash(b, sha1.New())
}

// MD5 返回稳定的 MD5 原始摘要.
// 相同输入在重启后和跨进程仍相同. 这是通用摘要辅助函数, 新代码也可直接调用 md5.Sum.
func MD5(b []byte) []byte {
	return Hash(b, nil)
}

// Hmac 使用 h 构造带密钥的 MAC, h 为 nil 时使用 MD5.
// h 应每次返回新的确定性密码学哈希实例; 算法及参数一致时, 相同输入和密钥在重启后及跨进程仍相同.
// nil 回退是兼容行为, 新调用方应显式传入 sha256.New 等哈希工厂.
func Hmac(b []byte, key []byte, h func() hash.Hash) []byte {
	if h == nil {
		h = md5.New
	}
	mac := hmac.New(h, key)
	mac.Write(b)

	return mac.Sum(nil)
}

// Hash 重置 h, 写入 b 并返回摘要; h 为 nil 时使用 MD5.
// 输出稳定性由 h 的算法及参数决定; MD5/SHA 等确定性算法对相同输入在重启后和跨进程仍相同.
// 带随机种子的实现不保证跨实例或跨进程相同; 本函数会修改 h, 不应并发共用同一实例.
func Hash(b []byte, h hash.Hash) []byte {
	if h == nil {
		h = md5.New()
	}
	h.Reset()
	h.Write(b)

	return h.Sum(nil)
}

// MustMD5Sum 返回稳定的文件 MD5, 任意错误都返回空字符串.
// 文件内容相同则重启后和跨进程仍相同. 缺失文件和目录因此无法区分, 需要识别失败时应使用 MD5Sum.
func MustMD5Sum(filename string) string {
	s, _ := MD5Sum(filename)
	return s
}

// MD5Sum 返回文件内容稳定的 MD5 小写十六进制摘要.
// 相同文件内容在重启后和跨进程仍相同. 目录当前返回空字符串和 nil, 其他状态或读取失败返回错误.
func MD5Sum(filename string) (string, error) {
	if info, err := os.Stat(filename); err != nil {
		return "", err
	} else if info.IsDir() {
		return "", nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = file.Close()
	}()

	return MD5Reader(file)
}

// MD5Reader 返回从 r 读取的全部字节的稳定 MD5.
// 相同输入字节在重启后和跨进程仍相同. 非 EOF 读取错误原样返回, 且不返回部分摘要.
func MD5Reader(r io.Reader) (string, error) {
	h := md5.New()
	// io.Copy 先写入本次读取的数据再处理 EOF, 避免丢失与 EOF 同时返回的末段字节.
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
