package xhash

import (
	"hash/maphash"
	"unsafe"
)

// MemHashb 返回进程内 runtime memhash.
// 同一进程内相同输入得到相同值, 但重启或其他进程可能不同; 仅适合进程内缓存键.
// nil 和空切片按空内容处理, 使用公开 unsafe API 直接传递数据指针, 保留原 seed 和算法.
func MemHashb(b []byte) uint64 {
	return uint64(memhash(unsafe.Pointer(unsafe.SliceData(b)), offset64, uintptr(len(b))))
}

// MemHash 返回进程内 runtime memhash.
// 同一进程内相同输入得到相同值, 但重启或其他进程可能不同; 不可持久化, 跨进程传输或用于稳定分片.
// 空字符串按空内容处理; memhash 在长度为零时不会解引用数据指针.
func MemHash(s string) uint64 {
	return uint64(memhash(unsafe.Pointer(unsafe.StringData(s)), offset64, uintptr(len(s))))
}

// MemHashb32 使用 offset32 初始化 runtime memhash 并截断为 32 位, 不保证等于截断 MemHashb 的结果.
// 同一进程内相同输入得到相同值, 但重启或其他进程可能不同, 也不能与 SumBytes32 互换.
func MemHashb32(b []byte) uint32 {
	return uint32(memhash(unsafe.Pointer(unsafe.SliceData(b)), offset32, uintptr(len(b))))
}

// MemHash32 使用 offset32 初始化 runtime memhash 并截断为 32 位, 不保证等于截断 MemHash 的结果.
// 同一进程内相同输入得到相同值, 但重启或其他进程可能不同; 只能在单个进程内使用, 稳定替代是 Sum32.
func MemHash32(s string) uint32 {
	return uint32(memhash(unsafe.Pointer(unsafe.StringData(s)), offset32, uintptr(len(s))))
}

// HashSeedString 直接返回 maphash.String(seed, s).
// 相同 seed 和输入在当前进程内结果相同; seed 必须由 maphash.MakeSeed 创建, 零值会 panic.
// seed 不能跨进程重建, 结果不可作为重启后或跨进程稳定的协议值.
func HashSeedString(seed maphash.Seed, s string) uint64 {
	return maphash.String(seed, s)
}

// HashSeedUint64 用进程内 maphash 种子混合 n.
// 相同 seed 和 n 在当前进程内结果相同, 但重启或其他进程可能不同; 算法也不同于 HashUint64.
// 该兼容实现读取 maphash.Seed 的内部表示, 不应作为新的稳定哈希接口.
func HashSeedUint64(seed maphash.Seed, n uint64) uint64 {
	// Java's Long standard hash function.
	n = n ^ (n >> 32)
	nseed := *(*uint64)(unsafe.Pointer(&seed))
	// 64-bit variation of boost's hash_combine.
	nseed ^= n + 0x9e3779b97f4a7c15 + (nseed << 12) + (nseed >> 4)
	return nseed
}

//go:noescape
//go:linkname memhash runtime.memhash
func memhash(p unsafe.Pointer, h, s uintptr) uintptr
