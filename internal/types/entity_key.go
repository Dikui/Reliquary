package types

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalizeEntityKey provides exact name matching across width, case and whitespace.
func NormalizeEntityKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(norm.NFKC.String(value))))
}
