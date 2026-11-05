package service

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

type productNameOwner struct {
	id      string
	enabled bool
}

type productMatcher struct {
	pattern *regexp.Regexp
	owners  map[string]productNameOwner
}

type productMatcherCache struct {
	mu    sync.Mutex
	items map[string]*productMatcher
}

// Callers hold the space identity barrier until their associations are saved.
func (c *productMatcherCache) load(tx *gorm.DB, library *types.Library) (*productMatcher, error) {
	ids := append([]string{}, library.ProductLibraryIDs...)
	if library.ManagesEntities() {
		ids = appendUniqueEntityString(ids, library.ID)
	}
	var targets []types.Library
	if len(ids) > 0 {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id IN ? AND space_id=? AND entity_mode=?", ids, library.SpaceID, types.EntityModeManaged).Order("id").Find(&targets).Error; err != nil {
			return nil, err
		}
	}
	ids = nil
	key := fmt.Sprintf("%d", library.SpaceID)
	for _, target := range targets {
		if slices.Contains(target.DocumentRelationTypes, "references") {
			ids = append(ids, target.ID)
			key += fmt.Sprintf("/%s:%d", target.ID, target.UpdatedAt.UnixNano())
		}
	}
	c.mu.Lock()
	cached := c.items[key]
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	var products []types.Entity
	if len(ids) > 0 {
		if err := tx.Select("id", "canonical_name", "code", "auto_match").Where("space_id=? AND library_id IN ? AND archived_at IS NULL AND merged_into_id=''", library.SpaceID, ids).Find(&products).Error; err != nil {
			return nil, err
		}
	}
	byID := make(map[string]*types.Entity, len(products))
	for i := range products {
		byID[products[i].ID] = &products[i]
	}
	if len(products) > 0 {
		var aliases []types.EntityAlias
		if err := tx.Table("entity_aliases a").Select("a.entity_id", "a.alias").
			Joins("JOIN entities e ON e.id=a.entity_id AND e.space_id=a.space_id").
			Where("e.space_id=? AND e.library_id IN ? AND e.archived_at IS NULL AND e.merged_into_id=''", library.SpaceID, ids).Scan(&aliases).Error; err != nil {
			return nil, err
		}
		for _, alias := range aliases {
			byID[alias.EntityID].Aliases = append(byID[alias.EntityID].Aliases, alias.Alias)
		}
	}
	matcher, err := compileProductMatcher(products)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached := c.items[key]; cached != nil {
		return cached, nil
	}
	// Bound retained catalog versions without a background eviction worker.
	if len(c.items) >= 32 || c.items == nil {
		c.items = map[string]*productMatcher{}
	}
	c.items[key] = matcher
	return matcher, nil
}

func productNameAutoMatches(name string, settings map[string]bool) bool {
	key := normalizeEntityName(name)
	if enabled, exists := settings[key]; exists {
		return enabled
	}
	if utf8.RuneCountInString(key) > 2 {
		return true
	}
	han, code := key != "", key != ""
	for _, r := range key {
		han = han && unicode.Is(unicode.Han, r)
		code = code && (r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}
	return !han && !code
}

// Use the same simple folding as Go's case-insensitive regexp, including e.g. ſ/S.
func productNameKey(name string) string {
	return strings.Map(func(r rune) rune {
		lowest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			lowest = min(lowest, next)
		}
		return lowest
	}, name)
}

func compileProductMatcher(products []types.Entity) (*productMatcher, error) {
	matcher := &productMatcher{owners: map[string]productNameOwner{}}
	for _, product := range products {
		for _, name := range append(append([]string{}, product.Aliases...), product.CanonicalName, product.Code) {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := productNameKey(name)
			owner, exists := matcher.owners[key]
			if exists && owner.id != product.ID {
				owner.id, owner.enabled = "", false
			} else if !exists {
				owner = productNameOwner{id: product.ID, enabled: productNameAutoMatches(name, product.AutoMatch)}
			}
			matcher.owners[key] = owner
		}
	}
	if len(matcher.owners) == 0 {
		return matcher, nil
	}
	names := make([]string, 0, len(matcher.owners))
	for name := range matcher.owners {
		names = append(names, regexp.QuoteMeta(name))
	}
	slices.Sort(names)
	pattern, err := regexp.Compile("(?i)(?:" + strings.Join(names, "|") + ")")
	if err != nil {
		return nil, err
	}
	pattern.Longest()
	matcher.pattern = pattern
	return matcher, nil
}

type productMention struct {
	id         string
	start, end int // byte offsets in the original body
}

func (m *productMatcher) matches(body string) []productMention {
	if m.pattern == nil {
		return nil
	}
	var result []productMention
	coveredEnd := 0
	for offset := 0; offset < len(body); {
		span := m.pattern.FindStringIndex(body[offset:])
		if span == nil {
			break
		}
		start, end := offset+span[0], offset+span[1]
		first, size := utf8.DecodeRuneInString(body[start:])
		// Advance from the start so partially overlapping names are also visited.
		offset = start + size
		before, _ := utf8.DecodeLastRuneInString(body[:start])
		if productWordRune(first) && productWordRune(before) {
			continue
		}
		for end > start {
			last, size := utf8.DecodeLastRuneInString(body[start:end])
			after, _ := utf8.DecodeRuneInString(body[end:])
			if productWordRune(last) && productWordRune(after) {
				// An invalid longer name must not hide a valid shorter prefix.
				shorter := m.pattern.FindStringIndex(body[start : end-size])
				if shorter == nil || shorter[0] != 0 {
					break
				}
				end = start + shorter[1]
				continue
			}
			// Only the longest valid name at each start can survive containment.
			// Disabled or ambiguous names still suppress fully contained names.
			if end > coveredEnd {
				coveredEnd = end
				owner := m.owners[productNameKey(body[start:end])]
				if owner.id != "" && owner.enabled {
					result = append(result, productMention{id: owner.id, start: start, end: end})
				}
			}
			break
		}
	}
	return result
}

func productWordRune(r rune) bool {
	return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r) || r == '_'
}

func (m *productMatcher) evidence(body string) map[string][]types.SourceLocation {
	mentions := m.matches(body)
	chunks := make([]*types.Chunk, 0, len(mentions))
	byteOffset, runeOffset := 0, 0
	for _, mention := range mentions {
		runeOffset += utf8.RuneCountInString(body[byteOffset:mention.start])
		end := runeOffset + utf8.RuneCountInString(body[mention.start:mention.end])
		chunks = append(chunks, &types.Chunk{Content: body[mention.start:mention.end], StartAt: runeOffset, EndAt: end})
		byteOffset = mention.start
	}
	if len(chunks) > 0 {
		types.AssignSourceLocations(body, chunks)
	}
	result := map[string][]types.SourceLocation{}
	for i, mention := range mentions {
		result[mention.id] = append(result[mention.id], chunks[i].SourceLocations...)
	}
	return result
}
