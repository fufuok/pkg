package xhash

import (
	"github.com/fufuok/pkg/utils"
)

const (
	// FNVa offset basis. See https://en.wikipedia.org/wiki/Fowler–Noll-Vo_hash_function#FNV-1a_hash
	offset32 = 2166136261
	offset64 = 14695981039346656037
	prime32  = 16777619
	prime64  = 1099511628211
)

// Sum64 返回稳定的 FNV-1a 64 位哈希.
// 相同输入在当前进程, 重启后和其他进程中都得到相同结果, 是 HashString64 的冻结基础算法.
func Sum64(s string) uint64 {
	return AddString64(offset64, s)
}

// SumBytes64 返回稳定的 FNV-1a 64 位哈希.
// 相同字节内容与 Sum64 一致, 重启后和跨进程仍相同, 可用于持久化分桶.
func SumBytes64(bs []byte) uint64 {
	return AddBytes64(offset64, bs)
}

// Sum32 返回稳定的 FNV-1a 32 位哈希.
// 相同输入在重启后和跨进程仍相同, DataRouter 等持久化路由决策依赖此精确结果.
func Sum32(s string) uint32 {
	return AddString32(offset32, s)
}

// SumBytes32 返回稳定的 FNV-1a 32 位哈希.
// 相同字节内容与 Sum32 一致, 重启后和跨进程仍相同, 不能替换为 maphash.
func SumBytes32(bs []byte) uint32 {
	return AddBytes32(offset32, bs)
}

// HashString64 拼接字符串并返回稳定的 FNV-1a 64 位哈希.
// 相同参数拼接后的结果在重启后和跨进程仍相同, 部署灰度阈值依赖此值.
// 拼接不添加分隔符, 因此不区分 ("ab", "c") 与 ("a", "bc") 的参数边界.
func HashString64(s ...string) uint64 {
	return Sum64(utils.JoinString(s...))
}

// HashString32 拼接字符串并返回稳定的 FNV-1a 32 位哈希.
// 相同参数拼接后的结果在重启后和跨进程仍相同, 但值域远小于 HashString64.
// 使用独立的 32 位 FNV 参数, 不是 HashString64 结果的低 32 位.
func HashString32(s ...string) uint32 {
	return Sum32(utils.JoinString(s...))
}

// HashBytes64 拼接字节并返回稳定的 FNV-1a 64 位哈希.
// 相同拼接内容在重启后和跨进程仍得到相同结果.
// 拼接不保留各参数边界, 相同字节内容与 HashString64 一致.
func HashBytes64(b ...[]byte) uint64 {
	return SumBytes64(utils.JoinBytes(b...))
}

// HashBytes32 拼接字节并返回稳定的 FNV-1a 32 位哈希.
// 相同拼接内容在重启后和跨进程仍得到相同结果; 仅当拼接字节表示相同字符串内容时, 才与 HashString32 一致.
func HashBytes32(b ...[]byte) uint32 {
	return SumBytes32(utils.JoinBytes(b...))
}

// HashUint64 返回 u 的 8 字节大端表示的稳定 FNV-1a 哈希.
// 相同 u 在重启后和跨进程仍得到相同结果, 不使用进程内 maphash 种子.
func HashUint64(u uint64) uint64 {
	return AddUint64(offset64, u)
}

// HashUint32 返回 u 的 4 字节大端表示的稳定 FNV-1a 哈希.
// 相同 u 在重启后和跨进程仍得到相同结果, 不是 runtime memhash.
func HashUint32(u uint32) uint32 {
	return AddUint32(offset32, u)
}

// AddString64 用 s 延续稳定的 FNV-1a 64 位哈希.
// 从相同初始 h 开始时, 相同 s 在重启后和跨进程结果仍相同.
func AddString64(h uint64, s string) uint64 {
	/*
		This is an unrolled version of this algorithm:
		for _, c := range s {
			h = (h ^ uint64(c)) * prime64
		}
		It seems to be ~1.5x faster than the simple loop in BenchmarkHash64:
		- BenchmarkHash64/hash_function-4   30000000   56.1 ns/op   642.15 MB/s   0 B/op   0 allocs/op
		- BenchmarkHash64/hash_function-4   50000000   38.6 ns/op   932.35 MB/s   0 B/op   0 allocs/op
	*/
	for len(s) >= 8 {
		h = (h ^ uint64(s[0])) * prime64
		h = (h ^ uint64(s[1])) * prime64
		h = (h ^ uint64(s[2])) * prime64
		h = (h ^ uint64(s[3])) * prime64
		h = (h ^ uint64(s[4])) * prime64
		h = (h ^ uint64(s[5])) * prime64
		h = (h ^ uint64(s[6])) * prime64
		h = (h ^ uint64(s[7])) * prime64
		s = s[8:]
	}

	if len(s) >= 4 {
		h = (h ^ uint64(s[0])) * prime64
		h = (h ^ uint64(s[1])) * prime64
		h = (h ^ uint64(s[2])) * prime64
		h = (h ^ uint64(s[3])) * prime64
		s = s[4:]
	}

	if len(s) >= 2 {
		h = (h ^ uint64(s[0])) * prime64
		h = (h ^ uint64(s[1])) * prime64
		s = s[2:]
	}

	if len(s) > 0 {
		h = (h ^ uint64(s[0])) * prime64
	}

	return h
}

// AddBytes64 用 b 延续稳定的 FNV-1a 64 位哈希.
// 字节语义与 AddString64 相同, 相同 h 和 b 在重启后和跨进程仍相同, 可用于持久化增量哈希.
func AddBytes64(h uint64, b []byte) uint64 {
	for len(b) >= 8 {
		h = (h ^ uint64(b[0])) * prime64
		h = (h ^ uint64(b[1])) * prime64
		h = (h ^ uint64(b[2])) * prime64
		h = (h ^ uint64(b[3])) * prime64
		h = (h ^ uint64(b[4])) * prime64
		h = (h ^ uint64(b[5])) * prime64
		h = (h ^ uint64(b[6])) * prime64
		h = (h ^ uint64(b[7])) * prime64
		b = b[8:]
	}

	if len(b) >= 4 {
		h = (h ^ uint64(b[0])) * prime64
		h = (h ^ uint64(b[1])) * prime64
		h = (h ^ uint64(b[2])) * prime64
		h = (h ^ uint64(b[3])) * prime64
		b = b[4:]
	}

	if len(b) >= 2 {
		h = (h ^ uint64(b[0])) * prime64
		h = (h ^ uint64(b[1])) * prime64
		b = b[2:]
	}

	if len(b) > 0 {
		h = (h ^ uint64(b[0])) * prime64
	}

	return h
}

// AddUint64 用 u 的大端字节延续稳定的 FNV-1a 哈希.
// 相同 h 和 u 在重启后和跨进程仍相同, 不依赖进程状态或 maphash 种子.
func AddUint64(h uint64, u uint64) uint64 {
	h = (h ^ ((u >> 56) & 0xFF)) * prime64
	h = (h ^ ((u >> 48) & 0xFF)) * prime64
	h = (h ^ ((u >> 40) & 0xFF)) * prime64
	h = (h ^ ((u >> 32) & 0xFF)) * prime64
	h = (h ^ ((u >> 24) & 0xFF)) * prime64
	h = (h ^ ((u >> 16) & 0xFF)) * prime64
	h = (h ^ ((u >> 8) & 0xFF)) * prime64
	h = (h ^ ((u >> 0) & 0xFF)) * prime64
	return h
}

// AddString32 用 s 延续稳定的 FNV-1a 32 位哈希.
// 从相同初始 h 开始时, 相同 s 在重启后和跨进程仍相同.
func AddString32(h uint32, s string) uint32 {
	for len(s) >= 8 {
		h = (h ^ uint32(s[0])) * prime32
		h = (h ^ uint32(s[1])) * prime32
		h = (h ^ uint32(s[2])) * prime32
		h = (h ^ uint32(s[3])) * prime32
		h = (h ^ uint32(s[4])) * prime32
		h = (h ^ uint32(s[5])) * prime32
		h = (h ^ uint32(s[6])) * prime32
		h = (h ^ uint32(s[7])) * prime32
		s = s[8:]
	}

	if len(s) >= 4 {
		h = (h ^ uint32(s[0])) * prime32
		h = (h ^ uint32(s[1])) * prime32
		h = (h ^ uint32(s[2])) * prime32
		h = (h ^ uint32(s[3])) * prime32
		s = s[4:]
	}

	if len(s) >= 2 {
		h = (h ^ uint32(s[0])) * prime32
		h = (h ^ uint32(s[1])) * prime32
		s = s[2:]
	}

	if len(s) > 0 {
		h = (h ^ uint32(s[0])) * prime32
	}

	return h
}

// AddBytes32 用 b 延续稳定的 FNV-1a 32 位哈希.
// 相同 h 和 b 在重启后和跨进程仍相同.
func AddBytes32(h uint32, b []byte) uint32 {
	for len(b) >= 8 {
		h = (h ^ uint32(b[0])) * prime32
		h = (h ^ uint32(b[1])) * prime32
		h = (h ^ uint32(b[2])) * prime32
		h = (h ^ uint32(b[3])) * prime32
		h = (h ^ uint32(b[4])) * prime32
		h = (h ^ uint32(b[5])) * prime32
		h = (h ^ uint32(b[6])) * prime32
		h = (h ^ uint32(b[7])) * prime32
		b = b[8:]
	}

	if len(b) >= 4 {
		h = (h ^ uint32(b[0])) * prime32
		h = (h ^ uint32(b[1])) * prime32
		h = (h ^ uint32(b[2])) * prime32
		h = (h ^ uint32(b[3])) * prime32
		b = b[4:]
	}

	if len(b) >= 2 {
		h = (h ^ uint32(b[0])) * prime32
		h = (h ^ uint32(b[1])) * prime32
		b = b[2:]
	}

	if len(b) > 0 {
		h = (h ^ uint32(b[0])) * prime32
	}

	return h
}

// AddUint32 用 u 的 4 字节大端表示延续稳定的 FNV-1a 哈希.
// 相同 h 和 u 在重启后和跨进程仍相同; 历史注释中的 8 字节描述不正确, 实际只处理 4 字节.
func AddUint32(h, u uint32) uint32 {
	h = (h ^ ((u >> 24) & 0xFF)) * prime32
	h = (h ^ ((u >> 16) & 0xFF)) * prime32
	h = (h ^ ((u >> 8) & 0xFF)) * prime32
	h = (h ^ ((u >> 0) & 0xFF)) * prime32
	return h
}
