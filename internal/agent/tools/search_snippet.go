package tools

import "strings"

const (
	snippetContextRunes  = 200
	snippetMaxMatchRunes = 200
	snippetMaxTotalRunes = 800
)

func searchQueryTokens(queries []string) []string {
	tokens := make([]string, 0, 8)
	seen := make(map[string]struct{})
	for _, q := range queries {
		for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
			switch r {
			case ' ', '\t', '\n', '\r', ',', '.', ';', ':', '?', '!',
				'(', ')', '[', ']', '{', '}', '"', '\'':
				return true
			}
			return false
		}) {
			tok = strings.ToLower(strings.TrimSpace(tok))
			if len([]rune(tok)) < 2 {
				continue
			}
			if _, ok := seen[tok]; ok {
				continue
			}
			seen[tok] = struct{}{}
			tokens = append(tokens, tok)
		}
	}
	return tokens
}
