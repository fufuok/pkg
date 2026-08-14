package xcrypto_test

import (
	"crypto/rand"
	"fmt"

	"github.com/fufuok/pkg/xcrypto"
)

// ExampleSeal 演示二进制封装: 同一密钥可解开每次都不同的密文.
func ExampleSeal() {
	key := make([]byte, xcrypto.AES256KeySize)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}

	first, err := xcrypto.Seal([]byte("hello"), key)
	if err != nil {
		panic(err)
	}
	second, err := xcrypto.Seal([]byte("hello"), key)
	if err != nil {
		panic(err)
	}

	plain, err := xcrypto.Open(first, key)
	if err != nil {
		panic(err)
	}

	fmt.Println(string(plain))
	fmt.Println(len(first) == 12+5+16)
	fmt.Println(len(first) == len(second))
	fmt.Println(string(first) != string(second))
	// Output:
	// hello
	// true
	// true
	// true
}

// ExampleSealString 演示文本封装, 输出可放入 URL 或配置值.
func ExampleSealString() {
	key := make([]byte, xcrypto.AES256KeySize)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}

	sealed, err := xcrypto.SealString("redis-password", key)
	if err != nil {
		panic(err)
	}
	plain, err := xcrypto.OpenString(sealed, key)
	if err != nil {
		panic(err)
	}

	fmt.Println(plain)
	fmt.Println(len(sealed) > 0)
	// Output:
	// redis-password
	// true
}
