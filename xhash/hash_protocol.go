package xhash

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"

	"github.com/fufuok/pkg/utils"
)

// HashString 拼接字符串并返回 HashString64 的稳定十进制形式.
// 相同参数在重启后和跨进程仍得到相同字符串; 现有代理 token 依赖此精确格式, 它不是密码学签名.
// 参数间不添加分隔符; 需要数值时直接使用 HashString64, 需要区分参数边界时应先明确编码.
func HashString(s ...string) string {
	return strconv.FormatUint(HashString64(s...), 10)
}

// HashBytes 拼接字节并返回 HashBytes64 的稳定十进制形式.
// 相同字节在重启后和跨进程仍得到相同字符串; 十进制形式用于展示或旧键, 数值调用方应使用 HashBytes64.
func HashBytes(b ...[]byte) string {
	return strconv.FormatUint(HashBytes64(b...), 10)
}

// MD5Hex 返回稳定的 MD5 小写十六进制摘要.
// 相同输入在重启后和跨进程仍得到相同字符串. 现有 token, 签名和 AES 密钥派生依赖此输出; MD5 不抗碰撞, 不应新增安全用途.
func MD5Hex(s string) string {
	b := md5.Sum(utils.S2B(s))
	return hex.EncodeToString(b[:])
}

// MD5BytesHex 返回稳定的 MD5 小写十六进制摘要.
// 相同字节在重启后和跨进程仍得到相同字符串. nil 和空输入都按空内容处理, 可持久化, 但不应新增安全用途.
// 需要原始 16 字节时使用 MD5; 相同内容的字符串可使用 MD5Hex, 输出格式一致.
func MD5BytesHex(bs []byte) string {
	b := md5.Sum(bs)
	return hex.EncodeToString(b[:])
}
