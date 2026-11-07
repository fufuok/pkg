// Package jsongen
// Copyright 2024 Joshua J Baker. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.
//
// https://github.com/tidwall/gjson
package jsongen

import (
	"slices"
	"unicode/utf8"

	"github.com/fufuok/pkg/utils"
)

// DisableEscapeHTML 禁用 JSON 编码时对 HTML 敏感字符的自动转义.
// 默认将 '>', '<' 和 '&' 分别转义为 \u003e, \u003c 和 \u0026.
// 这是 jsongen 的全局编码开关, 应在其他 jsongen 函数调用前一次性设置.
var DisableEscapeHTML = false

var hexchars = [...]byte{
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
	'a', 'b', 'c', 'd', 'e', 'f',
}

// needsJSONEscape 判断字符串是否包含必须转义的控制字符、引号、反斜杠、HTML 字符或特殊 UTF-8 序列.
// 返回 false 时, AppendJSONString(dst, s) 的输出严格等于 `"` + s + `"`.
func needsJSONEscape(s string) bool {
	escapeHTML := !DisableEscapeHTML
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < ' ' || c == '"' || c == '\\' {
			return true
		}
		if escapeHTML && (c == '<' || c == '>' || c == '&') {
			return true
		}
		if c >= utf8.RuneSelf {
			r, n := utf8.DecodeRuneInString(s[i:])
			if (r == utf8.RuneError && n == 1) || r == '\u2028' || r == '\u2029' {
				return true
			}
			i += n - 1
		}
	}
	return false
}

// EscapeString 转义 JSON 字符串, 并在前后加上双引号.
func EscapeString(s string) string {
	if !needsJSONEscape(s) {
		dst := make([]byte, len(s)+2)
		dst[0] = '"'
		copy(dst[1:], s)
		dst[len(s)+1] = '"'
		return utils.B2S(dst)
	}
	dst := make([]byte, 0, len(s)+8)
	return utils.B2S(appendEscapedJSONString(dst, s))
}

// AppendJSONString 将 s 编码为带双引号的合法 JSON 字符串并追加到 dst.
func AppendJSONString(dst []byte, s string) []byte {
	if !needsJSONEscape(s) {
		dst = slices.Grow(dst, len(s)+2)
		dst = append(dst, '"')
		dst = append(dst, s...)
		return append(dst, '"')
	}
	dst = slices.Grow(dst, len(s)+6)
	return appendEscapedJSONString(dst, s)
}

// appendEscapedJSONString 处理已知包含至少一处待转义字符的字符串编码.
//
//nolint:cyclop,nestif
func appendEscapedJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' {
			dst = append(dst, '\\')
			switch s[i] {
			case '\b':
				dst = append(dst, 'b')
			case '\f':
				dst = append(dst, 'f')
			case '\n':
				dst = append(dst, 'n')
			case '\r':
				dst = append(dst, 'r')
			case '\t':
				dst = append(dst, 't')
			default:
				dst = append(dst, 'u')
				dst = appendHex16(dst, uint16(s[i]))
			}
		} else if !DisableEscapeHTML &&
			(s[i] == '>' || s[i] == '<' || s[i] == '&') {
			dst = append(dst, '\\', 'u')
			dst = appendHex16(dst, uint16(s[i]))
		} else if s[i] == '\\' {
			dst = append(dst, '\\', '\\')
		} else if s[i] == '"' {
			dst = append(dst, '\\', '"')
		} else if s[i] > 127 {
			r, n := utf8.DecodeRuneInString(s[i:])
			if n == 0 {
				break
			}
			if r == utf8.RuneError && n == 1 {
				dst = append(dst, `\ufffd`...)
			} else if r == '\u2028' || r == '\u2029' {
				dst = append(dst, `\u202`...)
				dst = append(dst, hexchars[r&0xF])
			} else {
				dst = append(dst, s[i:i+n]...)
			}
			i = i + n - 1
		} else {
			dst = append(dst, s[i])
		}
	}
	return append(dst, '"')
}

func appendHex16(dst []byte, x uint16) []byte {
	return append(
		dst,
		hexchars[x>>12&0xF], hexchars[x>>8&0xF],
		hexchars[x>>4&0xF], hexchars[x>>0&0xF],
	)
}
