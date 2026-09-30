package xhash_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/maphash"
	"strings"

	"github.com/fufuok/freelru"
	"github.com/fufuok/pkg/xhash"
)

// ExampleSum64 展示稳定分桶函数族的相同输入结果.
// 这些函数不依赖进程随机种子, 相同输入在当前进程, 重启后和其他进程中都相同.
// Sum64/Sum32 处理字符串, SumBytes64/SumBytes32 对相同字节内容产生对应的相同值.
// HashString64/HashString32 和 HashBytes64/HashBytes32 先直接拼接参数, 不保留参数边界.
// AddString64/AddString32 与 AddBytes64/AddBytes32 从给定 h 继续计算, 可替代先拼接再 Sum.
// HashUint64/HashUint32 与 AddUint64/AddUint32 按固定宽度的大端字节处理整数, 不等于哈希十进制文本.
// 32 位和 64 位版本使用各自的 FNV 参数, 不能用截断 64 位结果替代 32 位版本.
func ExampleSum64() {
	const input = "12345"
	const (
		stableOffset64 uint64 = 14695981039346656037
		stableOffset32 uint32 = 2166136261
	)

	fmt.Println(xhash.Sum64(input))
	fmt.Println(xhash.SumBytes64([]byte(input)))
	fmt.Println(xhash.Sum32(input))
	fmt.Println(xhash.SumBytes32([]byte(input)))
	fmt.Println(xhash.HashString64(input))
	fmt.Println(xhash.HashString32(input))
	fmt.Println(xhash.HashBytes64([]byte(input)))
	fmt.Println(xhash.HashBytes32([]byte(input)))
	fmt.Println(xhash.Sum64(input) == xhash.AddString64(stableOffset64, input))
	fmt.Println(xhash.SumBytes64([]byte(input)) == xhash.AddBytes64(stableOffset64, []byte(input)))
	fmt.Println(xhash.Sum32(input) == xhash.AddString32(stableOffset32, input))
	fmt.Println(xhash.SumBytes32([]byte(input)) == xhash.AddBytes32(stableOffset32, []byte(input)))
	fmt.Println(xhash.HashUint64(1) == xhash.AddUint64(stableOffset64, 1))
	fmt.Println(xhash.HashUint32(1) == xhash.AddUint32(stableOffset32, 1))
	// 固定桶数和编码规则时, 同一键在各进程中落入相同的桶.
	fmt.Println("bucket:", xhash.Sum64(input)%100)
	// 分段计算可以避免为临时拼接分配字符串, 结果与一次性输入相同.
	fmt.Println(xhash.AddString64(xhash.Sum64("12"), "345") == xhash.Sum64(input))

	// Output:
	// 16534377278781491704
	// 16534377278781491704
	// 1136836824
	// 1136836824
	// 16534377278781491704
	// 1136836824
	// 16534377278781491704
	// 1136836824
	// true
	// true
	// true
	// true
	// true
	// true
	// bucket: 4
	// true
}

// ExampleHashString 展示协议使用的十进制字符串包装.
// HashString 和 HashBytes 对相同拼接内容在同一进程, 重启后和跨进程都得到相同字符串.
// 它们分别格式化 HashString64 和 HashBytes64 的结果, 数值计算可直接使用后两者.
// 拼接不加分隔符, 因而 ("12", "345") 与 ("12345") 相同; 需要区分字段边界时应先明确编码.
func ExampleHashString() {
	const input = "12345"

	fmt.Println(xhash.HashString(input))
	fmt.Println(xhash.HashBytes([]byte(input)))

	// Output:
	// 16534377278781491704
	// 16534377278781491704
}

// ExampleMD5Hex 展示协议使用的 MD5 十六进制输出.
// MD5Hex 与 MD5BytesHex 对相同内容在同一进程, 重启后和跨进程都相同.
// MD5 返回原始 16 字节, 经 hex.EncodeToString 后与这两个字符串助手一致.
// nil 字节切片和空输入都按空内容处理; MD5 仅用于兼容或非安全校验, 不适合新增安全设计.
func ExampleMD5Hex() {
	const input = "12345"

	fmt.Println(xhash.MD5Hex(input))
	fmt.Println(xhash.MD5BytesHex([]byte(input)))
	fmt.Println(hex.EncodeToString(xhash.MD5([]byte(input))) == xhash.MD5Hex(input))

	// Output:
	// 827ccb0eea8a706c4c34a16891f84e7b
	// 827ccb0eea8a706c4c34a16891f84e7b
	// true
}

// ExampleMD5Reader 展示流和文件摘要函数族.
// MD5Reader 从当前位置读到 EOF, 不负责关闭 r; 相同已读字节在同一进程, 重启后和跨进程的摘要相同.
// 文件路径可用 MD5Sum(filename), 它负责打开和关闭文件, 读取成功时等价于同内容的 MD5Reader.
// MD5Sum 遇到目录返回 ("", nil), 打开或读取失败返回错误; 不能只通过 err 判断结果是有效摘要.
// MustMD5Sum(filename) 忽略 MD5Sum 的错误并返回空字符串, 仅适合允许缺失摘要的场景.
func ExampleMD5Reader() {
	sum, err := xhash.MD5Reader(strings.NewReader("12345"))
	if err != nil {
		fmt.Println("read failed:", err)
		return
	}
	fmt.Println(sum)

	// Output: 827ccb0eea8a706c4c34a16891f84e7b
}

// ExampleSha256Hex 展示内容校验摘要.
// Sha256 接受字节并返回原始摘要, Sha256Hex 接受字符串并返回对应的小写十六进制文本.
// Sha512/Sha512Hex 和 Sha1/Sha1Hex 的原始与文本关系相同, 但算法和摘要长度不同, 不能直接互换结果.
// 每个算法对相同内容在同一进程, 重启后和跨进程都返回相同摘要; 无密钥摘要不是 MAC.
// Sha1/Sha1Hex 只用于兼容既有协议, 新的内容校验优先使用 Sha256/Sha256Hex.
func ExampleSha256Hex() {
	const input = "Fufu 中　文加密/解密~&#123a"

	fmt.Println(xhash.Sha256Hex(input))
	fmt.Println(hex.EncodeToString(xhash.Sha256([]byte(input))) == xhash.Sha256Hex(input))

	// Output:
	// ed3772cefd8991edac6d198df7b62c224b92038e2d435a9a1e2734211e5b5e0b
	// true
}

// ExampleHash 展示显式选择摘要算法以及兼容的 nil 默认值.
// Hash 会重置传入的 hash.Hash, 因此不能用来延续该实例已有的摘要状态, 也不要并发共享该实例.
// 使用 SHA-256 等确定性算法时, 相同内容在同一进程, 重启后和跨进程的结果相同.
// 对任意自定义 hash.Hash, 稳定性由该实例的算法和种子决定; 例如 maphash.Hash 不保证跨进程一致.
// Hash(b, nil) 使用 MD5, 与 MD5(b) 等价; 新调用方宜显式选算法或使用对应的 Sha256 等助手.
func ExampleHash() {
	input := []byte("12345")
	sum := xhash.Hash(input, sha256.New())
	fmt.Println(hex.EncodeToString(sum) == xhash.Sha256Hex(string(input)))
	fmt.Println(hex.EncodeToString(xhash.Hash(input, nil)) == xhash.MD5BytesHex(input))

	// Output:
	// true
	// true
}

// ExampleHmacSHA256Hex 展示带密钥的认证码.
// HmacSHA256 返回原始字节, HmacSHA256Hex 返回对应的小写十六进制文本.
// HmacSHA512/HmacSHA512Hex 和 HmacSHA1/HmacSHA1Hex 的原始与文本关系相同, 但算法及输出不同.
// 这些固定算法对相同消息和密钥在同一进程, 重启后和跨进程都得到相同 MAC, 新协议优先使用 HMAC-SHA-256.
// Hmac 允许显式传 sha256.New 等密码学哈希工厂, 传 nil 时兼容地使用 MD5; 必须与校验方约定相同算法.
// HmacSHA1/HmacSHA1Hex 仅用于兼容既有协议; 验证原始 MAC 时应使用 hmac.Equal, 不直接比较文本.
func ExampleHmacSHA256Hex() {
	const input = "Fufu 中　文加密/解密~&#123a"

	fmt.Println(xhash.HmacSHA256Hex(input, "Fufu"))
	fmt.Println(hex.EncodeToString(xhash.HmacSHA256([]byte(input), []byte("Fufu"))) == xhash.HmacSHA256Hex(input, "Fufu"))
	fmt.Println(hex.EncodeToString(xhash.Hmac([]byte(input), []byte("Fufu"), sha256.New)) == xhash.HmacSHA256Hex(input, "Fufu"))

	// Output:
	// 6d502095be042aab03ac7ae36dd0ca504e54eb72569547dca4e16e5de605ae7c
	// true
	// true
}

// ExampleMemHashb 展示当前进程内缓存键的用法.
// MemHash 与 MemHashb 分别接收字符串和字节, 同一进程内对相同内容的结果相同; MemHash32 与 MemHashb32 同理.
// 32 位接口使用不同的初始 seed, 不是截断 MemHash/MemHashb 的 64 位结果; 只能在选择相应宽度时替换.
// 这些结果不保证重启后或跨进程一致, 不能用作持久化键或稳定分桶; 新代码优先使用固定 seed 的 maphash.
func ExampleMemHashb() {
	input := []byte("request body")
	h1 := xhash.MemHashb(input)
	h2 := xhash.MemHashb(input)

	fmt.Println(h1 == h2)
	fmt.Println(h1 == xhash.MemHash(string(input)))
	fmt.Println(xhash.MemHashb32(input) == xhash.MemHash32(string(input)))

	// Output:
	// true
	// true
	// true
}

// ExampleHashSeedString 展示固定保存一个 maphash.Seed 的进程内用法.
// HashSeedString 等价于 maphash.String; seed 必须由 maphash.MakeSeed 创建并复用, 零 seed 会 panic.
// HashSeedUint64 是读取 seed 内部表示的旧整数混合算法, 不等于哈希整数文本或 maphash.Comparable.
// 两者对各自相同 seed 和输入在同一进程内结果相同, 但不保证重启后或跨进程相同, 不能用于稳定分桶.
// HashSeedUint64 仅用于兼容现有进程内调用, 新容器的整数键直接使用 maphash.Comparable.
func ExampleHashSeedString() {
	seed := maphash.MakeSeed()
	input := "cache key"

	h1 := xhash.HashSeedString(seed, input)
	h2 := xhash.HashSeedString(seed, input)
	n1 := xhash.HashSeedUint64(seed, 42)
	n2 := xhash.HashSeedUint64(seed, 42)
	fmt.Println(h1 == h2)
	fmt.Println(n1 == n2)

	// Output:
	// true
	// true
}

// Example_maphashComparable 展示通用容器键直接使用标准库.
// seed 在容器构造时生成一次; 同一进程内相同 seed 对按 == 相等的键产生相同哈希, 不保证重启或跨进程一致.
// NaN 不与自身相等, 重复哈希也不保证相同; 接口中的 slice/map/func 等不可比较动态值会 panic.
func Example_maphashComparable() {
	seed := maphash.MakeSeed()
	input := "map key"

	h1 := maphash.Comparable(seed, input)
	h2 := maphash.Comparable(seed, input)
	fmt.Println(h1 == h2)

	// Output: true
}

// Example_freelruMakeHasher 展示 freelru 直接创建默认键哈希函数.
// 同一进程内同一个 hasher 对按 == 相等的键产生相同 uint32 哈希; NaN 不与自身相等, 不保证重复结果一致.
// 每次创建 hasher 都选择新 seed, 不同实例, 重启后或跨进程不保证相同, 不应持久化结果.
// freelru 保留其类型检查, 含接口的键类型需要自定义哈希; 普通缓存使用 NewDefault 等构造器即可自动选择 hasher.
func Example_freelruMakeHasher() {
	hasher := freelru.MakeHasher[string]()

	fmt.Println(hasher("lru key") == hasher("lru key"))

	// Output: true
}
