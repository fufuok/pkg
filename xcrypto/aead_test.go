package xcrypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/fufuok/pkg/assert"
)

var (
	aeadKey128 = bytes.Repeat([]byte{0x11}, AES128KeySize)
	aeadKey256 = bytes.Repeat([]byte{0x22}, AES256KeySize)
)

// TestSealOpenRoundTrip 覆盖空值、二进制 0x00、中文和两种合法密钥长度的往返.
func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()

	plaintexts := [][]byte{
		nil,
		{},
		[]byte("hello"),
		[]byte("Fufu 中　文加密/解密~&#123a"),
		{0x00, 0x00, 0x01, 0x00},
		bytes.Repeat([]byte{0x00}, 32),
		bytes.Repeat([]byte("x"), 4096),
	}
	keys := [][]byte{aeadKey128, aeadKey256}

	for _, key := range keys {
		for _, plain := range plaintexts {
			sealed, err := Seal(plain, key)
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			if len(sealed) != aeadNonceSize+len(plain)+aeadTagSize {
				t.Fatalf("sealed length=%d, want %d", len(sealed), aeadNonceSize+len(plain)+aeadTagSize)
			}

			got, err := Open(sealed, key)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if len(plain) == 0 {
				assert.Equal(t, 0, len(got))
				continue
			}
			assert.Equal(t, plain, got)
		}
	}
}

// TestSealNonceRandomness 同一明文同一密钥必须得到不同密文, 且都能解回原文.
func TestSealNonceRandomness(t *testing.T) {
	t.Parallel()

	plain := []byte("same-plaintext")
	first, err := Seal(plain, aeadKey256)
	if err != nil {
		t.Fatalf("seal first: %v", err)
	}
	second, err := Seal(plain, aeadKey256)
	if err != nil {
		t.Fatalf("seal second: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("Seal returned identical ciphertext for the same plaintext")
	}
	if bytes.Equal(first[:aeadNonceSize], second[:aeadNonceSize]) {
		t.Fatal("Seal reused nonce")
	}

	gotFirst, err := Open(first, aeadKey256)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	gotSecond, err := Open(second, aeadKey256)
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	assert.Equal(t, plain, gotFirst)
	assert.Equal(t, plain, gotSecond)
}

// TestSealRejectsInvalidKey 只接受 16 / 32 字节密钥, 包括拒绝 AES-192.
func TestSealRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	keys := [][]byte{
		nil,
		{},
		[]byte("short"),
		bytes.Repeat([]byte{1}, 15),
		bytes.Repeat([]byte{1}, 17),
		bytes.Repeat([]byte{1}, 24),
		bytes.Repeat([]byte{1}, 31),
		bytes.Repeat([]byte{1}, 33),
	}
	for _, key := range keys {
		sealed, err := Seal([]byte("x"), key)
		assert.Nil(t, sealed)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("key len=%d err=%v, want ErrInvalidKey", len(key), err)
		}

		plain, err := Open(bytes.Repeat([]byte{1}, aeadNonceSize+aeadTagSize), key)
		assert.Nil(t, plain)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("open key len=%d err=%v, want ErrInvalidKey", len(key), err)
		}
	}
}

// TestOpenRejectsWrongKey 正确格式但密钥不匹配时必须认证失败.
func TestOpenRejectsWrongKey(t *testing.T) {
	t.Parallel()

	sealed, err := Seal([]byte("secret"), aeadKey256)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	plain, err := Open(sealed, aeadKey128)
	assert.Nil(t, plain)
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("err=%v, want ErrInvalidCiphertext", err)
	}

	other256 := bytes.Repeat([]byte{0x33}, AES256KeySize)
	plain, err = Open(sealed, other256)
	assert.Nil(t, plain)
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("err=%v, want ErrInvalidCiphertext", err)
	}
}

// TestOpenRejectsTamperedCiphertext 改 nonce、密文或 tag 的任意一字节都应失败.
func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()

	sealed, err := Seal([]byte("tamper-me"), aeadKey256)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	positions := []int{0, aeadNonceSize, len(sealed) - 1}
	for _, pos := range positions {
		cloned := bytes.Clone(sealed)
		cloned[pos] ^= 0x01
		plain, err := Open(cloned, aeadKey256)
		assert.Nil(t, plain)
		if !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("pos=%d err=%v, want ErrInvalidCiphertext", pos, err)
		}
	}
}

// TestOpenRejectsShortCiphertext 覆盖 nil、空、缺 nonce、缺 tag 的截断输入.
func TestOpenRejectsShortCiphertext(t *testing.T) {
	t.Parallel()

	inputs := [][]byte{
		nil,
		{},
		bytes.Repeat([]byte{1}, aeadNonceSize),
		bytes.Repeat([]byte{1}, aeadNonceSize+aeadTagSize-1),
	}
	for _, in := range inputs {
		plain, err := Open(in, aeadKey256)
		assert.Nil(t, plain)
		if !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("len=%d err=%v, want ErrInvalidCiphertext", len(in), err)
		}
	}
}

// TestOpenKnownVector 用标准库生成固定 nonce 密文, 确认 Seal 布局可被独立实现打开.
func TestOpenKnownVector(t *testing.T) {
	t.Parallel()

	plain := []byte("vector")
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	block, err := aes.NewCipher(aeadKey256)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}

	sealed := make([]byte, 0, len(nonce)+len(plain)+gcm.Overhead())
	sealed = append(sealed, nonce...)
	sealed = gcm.Seal(sealed, nonce, plain, nil)

	got, err := Open(sealed, aeadKey256)
	if err != nil {
		t.Fatalf("open known vector: %v", err)
	}
	assert.Equal(t, plain, got)
}

// TestSealDoesNotMutateInputs 确认 Seal 不改写调用方的明文和密钥缓冲.
func TestSealDoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	plain := []byte("mutable")
	key := bytes.Clone(aeadKey256)
	plainCopy := bytes.Clone(plain)
	keyCopy := bytes.Clone(key)

	if _, err := Seal(plain, key); err != nil {
		t.Fatalf("seal: %v", err)
	}
	assert.Equal(t, plainCopy, plain)
	assert.Equal(t, keyCopy, key)
}

// TestOpenDoesNotMutateCiphertext 确认认证失败和成功都不会改写输入密文.
func TestOpenDoesNotMutateCiphertext(t *testing.T) {
	t.Parallel()

	sealed, err := Seal([]byte("keep"), aeadKey256)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	original := bytes.Clone(sealed)

	if _, err = Open(sealed, aeadKey256); err != nil {
		t.Fatalf("open: %v", err)
	}
	assert.Equal(t, original, sealed)

	sealed[len(sealed)-1] ^= 0x01
	tampered := bytes.Clone(sealed)
	if _, err = Open(sealed, aeadKey256); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("tampered open err=%v", err)
	}
	assert.Equal(t, tampered, sealed)
}

// TestSealStringRoundTrip 文本封装必须保持 Unicode, 且每次编码结果不同.
func TestSealStringRoundTrip(t *testing.T) {
	t.Parallel()

	empty, err := SealString("", aeadKey256)
	if err != nil {
		t.Fatalf("seal empty string: %v", err)
	}
	gotEmpty, err := OpenString(empty, aeadKey256)
	if err != nil {
		t.Fatalf("open empty string: %v", err)
	}
	assert.Equal(t, "", gotEmpty)

	plain := "Fufu 中　文\n路径/密钥"
	first, err := SealString(plain, aeadKey256)
	if err != nil {
		t.Fatalf("seal string: %v", err)
	}
	second, err := SealString(plain, aeadKey256)
	if err != nil {
		t.Fatalf("seal string again: %v", err)
	}
	if first == second {
		t.Fatal("SealString returned identical text for the same plaintext")
	}
	if strings.ContainsAny(first, "+/=") {
		t.Fatalf("SealString should use raw URL encoding, got %q", first)
	}

	got, err := OpenString(first, aeadKey256)
	if err != nil {
		t.Fatalf("open string: %v", err)
	}
	assert.Equal(t, plain, got)
}

// TestOpenStringRejectsInvalidEncoding 只接受 Raw URL Base64, 拒绝填充和标准字母表.
func TestOpenStringRejectsInvalidEncoding(t *testing.T) {
	t.Parallel()

	sealed, err := Seal([]byte("text"), aeadKey256)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	cases := []string{
		"",
		"@@@",
		"abc+",
		base64.StdEncoding.EncodeToString(sealed),
		base64.URLEncoding.EncodeToString(sealed),
	}
	for _, in := range cases {
		got, err := OpenString(in, aeadKey256)
		assert.Equal(t, "", got)
		if !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("input=%q err=%v, want ErrInvalidCiphertext", in, err)
		}
	}
}

// TestSealStringRejectsInvalidKey 文本封装同样拒绝非法密钥, 避免调用方误把口令当 key.
func TestSealStringRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	got, err := SealString("x", []byte("password"))
	assert.Equal(t, "", got)
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err=%v, want ErrInvalidKey", err)
	}

	sealed, err := SealString("x", aeadKey256)
	if err != nil {
		t.Fatalf("seal string: %v", err)
	}
	got, err = OpenString(sealed, []byte("password"))
	assert.Equal(t, "", got)
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("open string invalid key err=%v, want ErrInvalidKey", err)
	}
}

// TestOpenStringRejectsWrongKey 文本密文换密钥必须失败, 且不返回原文.
func TestOpenStringRejectsWrongKey(t *testing.T) {
	t.Parallel()

	sealed, err := SealString("env-value", aeadKey256)
	if err != nil {
		t.Fatalf("seal string: %v", err)
	}

	got, err := OpenString(sealed, aeadKey128)
	assert.Equal(t, "", got)
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("err=%v, want ErrInvalidCiphertext", err)
	}
}

// TestSealIndependentFromEncrypt 旧配置密文不能被 Open, 新密文也不能被 Decrypt.
func TestSealIndependentFromEncrypt(t *testing.T) {
	t.Parallel()

	plain := "config-secret"
	legacy := Encrypt(plain, "0123456789012345")
	got, err := OpenString(legacy, aeadKey256)
	assert.Equal(t, "", got)
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("open encrypt output err=%v, want ErrInvalidCiphertext", err)
	}

	sealed, err := SealString(plain, aeadKey256)
	if err != nil {
		t.Fatalf("seal string: %v", err)
	}
	if Decrypt(sealed, "0123456789012345") == plain {
		t.Fatal("Decrypt must not open SealString output")
	}
}

// TestSealOpenConcurrent 并发 Seal / Open 不得互相干扰.
func TestSealOpenConcurrent(t *testing.T) {
	t.Parallel()

	const workers = 32
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := range workers {
		wg.Go(func() {
			plain := []byte{byte(i), 'A', 0x00, 'Z'}
			sealed, err := Seal(plain, aeadKey256)
			if err != nil {
				errCh <- err
				return
			}
			got, err := Open(sealed, aeadKey256)
			if err != nil {
				errCh <- err
				return
			}
			if !bytes.Equal(plain, got) {
				errCh <- errors.New("concurrent round trip mismatch")
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
