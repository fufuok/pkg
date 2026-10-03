// Package match provides a simple pattern matcher with unicode support.
package match

import (
	"unicode/utf8"
)

const (
	rNoMatch result = iota
	rMatch
	rStop
)

var maxRuneBytes = [...]byte{244, 143, 191, 191}

// Match returns true if str matches pattern. This is a very
// simple wildcard match where '*' matches on any number characters
// and '?' matches on any one character.
//
// pattern:
//
//	{ term }
//
// term:
//
//	'*'         matches any sequence of non-Separator characters
//	'?'         matches any single non-Separator character
//	c           matches character c (c != '*', '?', '\\')
//	'\\' c      matches character c
//
// 默认保持大小写敏感, 不对匹配复杂度设置上限.
func Match(str, pattern string) bool {
	return match0(str, pattern, false)
}

// MatchNoCase is the same as Match but performs a case-insensitive match.
// Such that string "Hello World" with match with lower case pattern "hello*"
// 仅忽略 ASCII 字母大小写, 其他 Unicode 字符仍按原值匹配.
func MatchNoCase(str, pattern string) bool {
	return match0(str, pattern, true)
}

// match0 统一普通和 ASCII 大小写不敏感匹配, 此入口不限制匹配复杂度.
func match0(str, pattern string, nocase bool) bool {
	if pattern == "*" {
		return true
	}
	return match(str, pattern, 0, nil, -1, nocase) == rMatch
}

// MatchLimit is the same as Match but will limit the complexity of the match
// operation. This is to avoid long running matches, specifically to avoid ReDos
// attacks from arbritary inputs.
//
// How it works:
// The underlying match routine is recursive and may call itself when it
// encounters a sandwiched wildcard pattern, such as: `user:*:name`.
// Everytime it calls itself a counter is incremented.
// The operation is stopped when counter > maxcomp*len(str).
// 大小写敏感匹配与忽略大小写入口共用预算规则, 超限时通过 stopped 标识.
func MatchLimit(str, pattern string, maxcomp int) (matched, stopped bool) {
	return matchLimit0(str, pattern, maxcomp, false)
}

// MatchLimitNoCase 使用与 MatchLimit 相同的复杂度预算, 仅忽略 ASCII 字母大小写.
// 超出预算时返回 stopped, 调用方应将其与普通不匹配区分.
func MatchLimitNoCase(str, pattern string, maxcomp int,
) (matched, stopped bool) {
	return matchLimit0(str, pattern, maxcomp, true)
}

// matchLimit0 共享普通和忽略大小写模式的计数器, 保留 MatchLimit 的终止语义.
func matchLimit0(str, pattern string, maxcomp int, nocase bool,
) (matched, stopped bool) {
	if pattern == "*" {
		return true, false
	}
	counter := 0
	r := match(str, pattern, len(str), &counter, maxcomp, nocase)
	if r == rStop {
		return false, true
	}
	return r == rMatch, false
}

type result int

// tolower 只折叠 ASCII 大写字母, 不执行 Unicode 大小写归一化.
func tolower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

// match 递归匹配通配符, 所有递归分支共享复杂度预算和大小写模式.
func match(str, pat string, slen int, counter *int, maxcomp int, nocase bool,
) result {
	// check complexity limit
	if maxcomp > -1 {
		if *counter > slen*maxcomp {
			return rStop
		}
		*counter++
	}

	for len(pat) > 0 {
		var wild bool
		pc, ps := rune(pat[0]), 1
		if pc > 0x7f {
			pc, ps = utf8.DecodeRuneInString(pat)
		}
		var sc rune
		var ss int
		if len(str) > 0 {
			sc, ss = rune(str[0]), 1
			if sc > 0x7f {
				sc, ss = utf8.DecodeRuneInString(str)
			}
		}
		switch pc {
		case '?':
			if ss == 0 {
				return rNoMatch
			}
		case '*':
			// Ignore repeating stars.
			for len(pat) > 1 && pat[1] == '*' {
				pat = pat[1:]
			}

			// If this star is the last character then it must be a match.
			if len(pat) == 1 {
				return rMatch
			}

			// Match and trim any non-wildcard suffix characters.
			var ok bool
			str, pat, ok = matchTrimSuffix(str, pat, nocase)
			if !ok {
				return rNoMatch
			}

			// Check for single star again.
			if len(pat) == 1 {
				return rMatch
			}

			// Perform recursive wildcard search.
			r := match(str, pat[1:], slen, counter, maxcomp, nocase)
			if r != rNoMatch {
				return r
			}
			if len(str) == 0 {
				return rNoMatch
			}
			wild = true
		default:
			if ss == 0 {
				return rNoMatch
			}
			if pc == '\\' {
				pat = pat[ps:]
				pc, ps = utf8.DecodeRuneInString(pat)
				if ps == 0 {
					return rNoMatch
				}
			}
			if nocase {
				sc, pc = tolower(sc), tolower(pc)
			}
			if sc != pc {
				return rNoMatch
			}
		}
		str = str[ss:]
		if !wild {
			pat = pat[ps:]
		}
	}
	if len(str) == 0 {
		return rMatch
	}
	return rNoMatch
}

// matchTrimSuffix matches and trims any non-wildcard suffix characters.
// Returns the trimed string and pattern.
//
// This is called because the pattern contains extra data after the wildcard
// star. Here we compare any suffix characters in the pattern to the suffix of
// the target string. Basically a reverse match that stops when a wildcard
// character is reached. This is a little trickier than a forward match because
// we need to evaluate an escaped character in reverse.
//
// Any matched characters will be trimmed from both the target
// string and the pattern.
// 大小写模式与前向匹配一致, 反向扫描仍保留通配符和转义边界.
func matchTrimSuffix(str, pat string, nocase bool) (string, string, bool) {
	// It's expected that the pattern has at least two bytes and the first byte
	// is a wildcard star '*'
	match := true
	for len(str) > 0 && len(pat) > 1 {
		pc, ps := utf8.DecodeLastRuneInString(pat)
		var esc bool
		for i := 0; ; i++ {
			if pat[len(pat)-ps-i-1] != '\\' {
				if i&1 == 1 {
					esc = true
					ps++
				}
				break
			}
		}
		if pc == '*' && !esc {
			match = true
			break
		}
		sc, ss := utf8.DecodeLastRuneInString(str)
		if nocase {
			pc, sc = tolower(pc), tolower(sc)
		}
		if !((pc == '?' && !esc) || pc == sc) {
			match = false
			break
		}
		str = str[:len(str)-ss]
		pat = pat[:len(pat)-ps]
	}
	return str, pat, match
}

// Allowable parses the pattern and determines the minimum and maximum allowable
// values that the pattern can represent.
// When the max cannot be determined, 'true' will be returned
// for infinite.
func Allowable(pattern string) (min, max string) {
	if pattern == "" || pattern[0] == '*' {
		return "", ""
	}

	minb := make([]byte, 0, len(pattern))
	maxb := make([]byte, 0, len(pattern))
	var wild bool
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' {
			wild = true
			break
		}
		if pattern[i] == '?' {
			minb = append(minb, 0)
			maxb = append(maxb, maxRuneBytes[:]...)
		} else {
			minb = append(minb, pattern[i])
			maxb = append(maxb, pattern[i])
		}
	}
	if wild {
		r, n := utf8.DecodeLastRune(maxb)
		if r != utf8.RuneError {
			if r < utf8.MaxRune {
				r++
				if r > 0x7f {
					b := make([]byte, 4)
					nn := utf8.EncodeRune(b, r)
					maxb = append(maxb[:len(maxb)-n], b[:nn]...)
				} else {
					maxb = append(maxb[:len(maxb)-n], byte(r))
				}
			}
		}
	}
	return string(minb), string(maxb)
}

// IsPattern returns true if the string is a pattern.
func IsPattern(str string) bool {
	for i := 0; i < len(str); i++ {
		if str[i] == '*' || str[i] == '?' {
			return true
		}
	}
	return false
}
