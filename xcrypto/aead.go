package xcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	// AES128KeySize 是 Seal / Open 接受的 AES-128 密钥长度.
	AES128KeySize = 16
	// AES256KeySize 是 Seal / Open 推荐的 AES-256 密钥长度.
	AES256KeySize = 32

	// aeadNonceSize 是 AES-GCM 标准 nonce 长度. 每次 Seal 都重新抽取, 并前置进密文.
	aeadNonceSize = 12
	// aeadTagSize 是 cipher.NewGCM 的默认认证标签长度.
	aeadTagSize = 16
)

var (
	// ErrInvalidKey 表示密钥长度不是 16 或 32 字节.
	// 调用方应使用独立随机密钥或 HKDF / Argon2 派生结果, 不要传入口令或 MD5Hex(secret).
	ErrInvalidKey = errors.New("invalid AEAD key")
	// ErrInvalidCiphertext 表示密文过短、文本编码损坏、密钥错误或认证失败.
	// Open / OpenString 在这些情况下不返回部分明文.
	ErrInvalidCiphertext = errors.New("invalid AEAD ciphertext")
)

// Seal 用 AES-GCM 封装明文, 返回 nonce || ciphertext || tag.
//
// 这是 pkg 推荐的通用加密入口, 不是 Encrypt 的升级版. 配置密钥、环境变量和
// BASE_SECRET_KEY 必须继续使用 Encrypt / Decrypt / GetenvDecrypt.
//
// 契约:
//   - key 必须是 16 或 32 字节; 推荐 32 字节 AES-256. 非法长度返回 ErrInvalidKey.
//   - nonce 固定 12 字节, 来自 crypto/rand, 每次调用都重新生成并写在密文最前面.
//   - 同一明文 + 同一密钥会得到不同密文; 同一密钥可以解开这些密文, 得到同一明文.
//   - 不接受调用方传入 nonce, 也不把上下文拼进 key. 需要绑定租户或路由时再使用后续 WithAD.
//   - nil 与空明文都合法, 结果仍包含 nonce 和 tag, 长度至少 28 字节.
//   - 失败返回 error, 不吞错, 不返回半段密文.
func Seal(plaintext, key []byte) ([]byte, error) {
	aead, err := newAESGCM(key)
	if err != nil {
		return nil, err
	}

	// 先为 nonce 留出前缀, 再让 Seal 把密文和 tag 追加在后面.
	// nonce 参数与 dst 前缀共享同一段内存, 这是标准库 AEAD 的常规写法.
	out := make([]byte, aeadNonceSize, aeadNonceSize+len(plaintext)+aead.Overhead())
	if _, err = rand.Read(out[:aeadNonceSize]); err != nil {
		return nil, fmt.Errorf("read AEAD nonce: %w", err)
	}

	return aead.Seal(out, out[:aeadNonceSize], plaintext, nil), nil
}

// Open 解开 Seal 产出的密文.
//
// 输入必须是 nonce(12) || ciphertext || tag(16). 密钥必须与 Seal 时完全相同.
// 密文过短、被改、密钥错误或认证失败都返回 ErrInvalidCiphertext, 不返回部分明文.
func Open(ciphertext, key []byte) ([]byte, error) {
	aead, err := newAESGCM(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aeadNonceSize+aead.Overhead() {
		return nil, ErrInvalidCiphertext
	}

	nonce := ciphertext[:aeadNonceSize]
	sealed := ciphertext[aeadNonceSize:]
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		// 不向外暴露标准库认证失败细节, 避免调用方把错误文本当控制流.
		return nil, ErrInvalidCiphertext
	}

	return plaintext, nil
}

// SealString 封装明文字符串, 再做 base64.RawURLEncoding.
// 二进制布局与 Seal 相同; 适合放入 URL、Cookie 或文本配置, 不适合替代 Encrypt.
func SealString(plaintext string, key []byte) (string, error) {
	sealed, err := Seal([]byte(plaintext), key)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// OpenString 解开 SealString 产出的文本密文.
// 只接受不带填充的 Raw URL Base64; 标准 Base64、填充 '-' / '_' 变体或损坏文本返回 ErrInvalidCiphertext.
func OpenString(ciphertext string, key []byte) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	plaintext, err := Open(sealed, key)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// newAESGCM 校验密钥长度并构造标准 AES-GCM.
// 只接受 16 / 32 字节, 拒绝 AES-192 和任意口令派生捷径, 把误用挡在 AEAD 之外.
func newAESGCM(key []byte) (cipher.AEAD, error) {
	switch len(key) {
	case AES128KeySize, AES256KeySize:
	default:
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init AES-GCM: %w", err)
	}

	return aead, nil
}
