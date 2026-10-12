package utils

import (
	"strings"
	"unicode/utf8"
)

// CleanInvalidUTF8 移除字符串中的非法 UTF-8 字符和 \x00
func CleanInvalidUTF8(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// 非法 UTF-8 字节，跳过
			i++
			continue
		}
		if r == 0 {
			// NULL 字符 \x00，跳过
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}

	return b.String()
}
