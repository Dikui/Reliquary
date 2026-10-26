package service

import (
	"encoding/json"
	"reliquary/internal/types"
	"strings"
	"unicode/utf8"
)

// Names and literal quotes keep entity selection grounded in the submitted material.
type understandingEntityMention struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	EntityID string `json:"entity_id"`
	Quote    string `json:"quote"`
}

func understandingEntityMentions(raw json.RawMessage) []understandingEntityMention {
	var values []json.RawMessage
	_ = json.Unmarshal(raw, &values)
	mentions := []understandingEntityMention{}
	for _, value := range values {
		var mention understandingEntityMention
		if json.Unmarshal(value, &mention) == nil {
			mentions = append(mentions, mention)
		}
	}
	return mentions
}

func qualifyUnderstandingEntityMentions(result *articleProcessingResult, body string) {
	accepted := []understandingEntityMention{}
	seen := map[string]bool{}
	for _, mention := range result.EntityMentions {
		mention.Name = strings.TrimSpace(mention.Name)
		mention.Type = types.NormalizeEntityType(mention.Type)
		mention.Quote = strings.TrimSpace(mention.Quote)
		key := types.NormalizeEntityKey(mention.Name)
		if key == "" || mention.Type == "" || utf8.RuneCountInString(mention.Name) > 512 ||
			utf8.RuneCountInString(key) > 512 || strings.ContainsAny(mention.Name, "\r\n\x00") ||
			mention.Quote == "" || !strings.Contains(body, mention.Quote) ||
			!strings.Contains(types.NormalizeEntityKey(mention.Quote), key) || seen[key] {
			continue
		}
		seen[key] = true
		accepted = append(accepted, mention)
		if len(accepted) == 3 {
			break
		}
	}
	result.EntityMentions = accepted
}

// The merge may select evidence from a segment, but cannot invent new evidence.
func qualifyMergedEntityMentions(result *articleProcessingResult, parts []*articleProcessingResult) {
	accepted := []understandingEntityMention{}
	seen := map[string]bool{}
	for _, selected := range result.EntityMentions {
		key := types.NormalizeEntityKey(selected.Name)
		if seen[key] {
			continue
		}
		for _, part := range parts {
			for _, mention := range part.EntityMentions {
				if key == types.NormalizeEntityKey(mention.Name) && selected.Quote == mention.Quote {
					accepted = append(accepted, mention)
					seen[key] = true
					break
				}
			}
			if seen[key] {
				break
			}
		}
		if len(accepted) == 3 {
			break
		}
	}
	result.EntityMentions = accepted
}
