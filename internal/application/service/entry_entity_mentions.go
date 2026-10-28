package service

import (
	"encoding/json"
	"fmt"
	"reliquary/internal/types"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Names and literal quotes keep entity selection grounded in the submitted material.
type understandingEntityMention struct {
	Name        string `json:"name"`
	SurfaceName string `json:"surface_name,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	EntityID    string `json:"entity_id"`
	Quote       string `json:"quote"`
}

func entitySeriesText(value string) string {
	value = norm.NFKC.String(value)
	return strings.NewReplacer("‐", "-", "‑", "-", "–", "-", "—", "-", "−", "-").Replace(strings.ToLower(strings.Join(strings.Fields(value), " ")))
}

// A version-like tail starts outside a word and carries a number, as in
// "Name 2", "Name-4.5" or "Name Pro 3"; "Namesake 2" and "Name Build" do not.
func entityVersionTail(rest string) bool {
	first, _ := utf8.DecodeRuneInString(rest)
	return rest != "" && !unicode.IsLetter(first) && strings.ContainsFunc(rest, unicode.IsDigit)
}

// catalogEntityMatch resolves an original name against the closed catalogue.
// An exact canonical name or alias wins; otherwise the longest catalogue name
// that prefixes it with a version-like tail is used. Ties match nothing.
func catalogEntityMatch(surface string, options []understandingEntityOption) *understandingEntityOption {
	text := entitySeriesText(surface)
	if text == "" {
		return nil
	}
	compact := strings.ReplaceAll(text, " ", "")
	exact, prefix, longest := map[int]bool{}, map[int]bool{}, 0
	for i, option := range options {
		for _, name := range append([]string{option.CanonicalName}, option.Aliases...) {
			candidate := entitySeriesText(name)
			if candidate == "" {
				continue
			}
			if strings.ReplaceAll(candidate, " ", "") == compact {
				exact[i] = true
				continue
			}
			rest, ok := strings.CutPrefix(text, candidate)
			if !ok || !entityVersionTail(rest) || len(candidate) < longest {
				continue
			}
			if len(candidate) > longest {
				longest, prefix = len(candidate), map[int]bool{}
			}
			prefix[i] = true
		}
	}
	for _, matches := range []map[int]bool{exact, prefix} {
		if len(matches) > 1 {
			return nil
		}
		for i := range matches {
			return &options[i]
		}
	}
	return nil
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

// Invalid entity evidence is discarded without failing the rest of the material.
// The three-entity cap applies after catalogue matching, so unmatched names
// cannot crowd out catalogue entities.
func qualifyUnderstandingEntityMentions(result *articleProcessingResult, body string) {
	accepted := []understandingEntityMention{}
	seen := map[string]bool{}
	_, blocks := articleEvidenceBody(body)
	runes := []rune(body)
	for _, mention := range result.EntityMentions {
		mention.Name = strings.TrimSpace(mention.Name)
		surface := strings.TrimSpace(mention.SurfaceName)
		if surface == "" {
			surface = mention.Name
		}
		mention.Quote = strings.TrimSpace(mention.Quote)
		if mention.SourceID != "" {
			number, err := strconv.Atoi(strings.TrimPrefix(mention.SourceID, "P"))
			if err != nil || mention.SourceID != fmt.Sprintf("P%d", number) || number < 1 || number > len(blocks) {
				continue
			}
			block := blocks[number-1]
			// Resolve the citation from stored text, never from the model's transcription.
			mention.Quote = strings.TrimSpace(string(runes[block.Start:block.End]))
		}
		key := types.NormalizeEntityKey(surface)
		if key == "" || utf8.RuneCountInString(surface) > 512 ||
			utf8.RuneCountInString(key) > 512 || strings.ContainsAny(surface, "\r\n\x00") ||
			mention.Quote == "" || !strings.Contains(body, mention.Quote) ||
			!strings.Contains(types.NormalizeEntityKey(mention.Quote), key) {
			continue
		}
		mention.SurfaceName = surface
		if seen[key] {
			continue
		}
		seen[key] = true
		accepted = append(accepted, mention)
	}
	result.EntityMentions = accepted
}

func entityMentionReference(segment, index int) string {
	return fmt.Sprintf("S%dE%d", segment+1, index+1)
}

// The merge may select evidence from a segment, but cannot invent new evidence.
func qualifyMergedEntityMentions(result *articleProcessingResult, parts []*articleProcessingResult) {
	accepted := []understandingEntityMention{}
	seen := map[string]bool{}
	references := map[string]understandingEntityMention{}
	for i, part := range parts {
		for j, mention := range part.EntityMentions {
			references[entityMentionReference(i, j)] = mention
		}
	}
	for _, selected := range result.EntityMentions {
		if selected.SourceID != "" {
			mention, ok := references[selected.SourceID]
			if !ok {
				continue
			}
			key := types.NormalizeEntityKey(mention.Name)
			if !seen[key] {
				accepted = append(accepted, mention)
				seen[key] = true
			}
			if len(accepted) == 3 {
				break
			}
			continue
		}
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
