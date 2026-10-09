package service

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
	"reliquary/internal/types"
)

// normalizeContextFocusText deliberately stays separate from object identity
// normalization. Prechecking tolerates width, whitespace, and hyphen spelling
// differences without changing P3 identity resolution or stored aliases.
func normalizeContextFocusText(value string, foldCase bool) string {
	value = width.Fold.String(value)
	value = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Dash, r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if foldCase {
		value = strings.ToLower(value)
	}
	return value
}

// contextFocusNameMatches checks one canonical name or alias against a body.
// CJK names match substrings; alphanumeric names and phrases need word edges.
// A short alphanumeric name retains its original case after width folding.
func contextFocusNameMatches(body, name string) bool {
	name = normalizeContextFocusText(name, false)
	if name == "" {
		return false
	}
	hasCJK, alphanumeric, lettersAndDigits := false, true, 0
	for _, r := range name {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			hasCJK = true
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			lettersAndDigits++
		} else if r != ' ' {
			alphanumeric = false
		}
	}
	short := !hasCJK && alphanumeric && lettersAndDigits <= 3 && !strings.Contains(name, " ")
	body = normalizeContextFocusText(body, !short)
	if !short {
		name = strings.ToLower(name)
	}
	if hasCJK || !alphanumeric {
		return strings.Contains(body, name)
	}
	for offset := 0; offset <= len(body)-len(name); {
		found := strings.Index(body[offset:], name)
		if found < 0 {
			return false
		}
		start := offset + found
		end := start + len(name)
		before, after := rune(0), rune(0)
		if start > 0 {
			before, _ = utf8.DecodeLastRuneInString(body[:start])
		}
		if end < len(body) {
			after, _ = utf8.DecodeRuneInString(body[end:])
		}
		if !contextFocusWordRune(before) && !contextFocusWordRune(after) {
			return true
		}
		_, size := utf8.DecodeRuneInString(body[start:])
		offset = start + size
	}
	return false
}

func contextFocusWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// PrechecksFocus reads existing focus and identities before the extraction
// model sees the source. MatchesFocus remains the authoritative post-resolution
// check, including accepted intake labels and explicit user label decisions.
func (s *KnowledgeContextService) PrechecksFocus(ctx context.Context, k *types.Knowledge, body string) (bool, error) {
	topicHit, err := s.MatchesFocus(ctx, k, nil)
	if err != nil || topicHit {
		return topicHit, err
	}
	focus, err := s.FocusScope(ctx, k.KnowledgeBaseID, types.KnowledgeContextQuery{})
	if err != nil {
		return false, err
	}
	ids := []string{}
	names := []string{}
	for _, entity := range focus.StarredEntities {
		ids = append(ids, entity.ID)
		names = append(names, entity.CanonicalName)
	}
	for _, entity := range focus.TrackedEntities {
		ids = append(ids, entity.ID)
		names = append(names, entity.CanonicalName)
	}
	if len(ids) > 0 {
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Select("alias").Where("tenant_id = ? AND entity_id IN ?", k.TenantID, uniqueContextIDs(ids)).Find(&aliases).Error; err != nil {
			return false, err
		}
		for _, alias := range aliases {
			names = append(names, alias.Alias)
		}
	}
	for _, name := range names {
		if contextFocusNameMatches(body, name) {
			return true, nil
		}
	}
	return false, nil
}
