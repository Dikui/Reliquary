package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"reliquary/internal/types"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Names and literal quotes keep entity selection grounded in the submitted material.
type understandingEntityMention struct {
	Name        string `json:"name"`
	SurfaceName string `json:"surface_name,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	EntityID    string `json:"entity_id"`
	Quote       string `json:"quote"`
}

var modelSeriesPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"GPT", regexp.MustCompile(`^gpt(?:[- ]?[0-9]+(?:\.[0-9]+)*|[- ](?:live|rosalind))(?:$|[- ])`)},
	{"Claude", regexp.MustCompile(`^claude[- ]?(?:(?:opus|sonnet|haiku)[- ]?)?[0-9]+(?:\.[0-9]+)*(?:$|[- ])`)},
	{"DeepSeek", regexp.MustCompile(`^deepseek[- ]?(?:[vr][0-9]+(?:\.[0-9]+)*|chat|reasoner|ocr|math)(?:$|[- (0-9])`)},
	{"Grok", regexp.MustCompile(`^grok(?:[- ]?[0-9]+(?:\.[0-9]+)*|[- ]code[- ]fast(?:[- ][0-9]+)?)(?:$|[- ])`)},
	{"Grok", regexp.MustCompile(`^(?:grok[- ]imagine|imagine[- ](?:image|video))(?:$|[- ])`)},
	{"Grok", regexp.MustCompile(`^grok[- ](?:voice|speech[- ]to[- ]text|text[- ]to[- ]speech)(?:$|[- (])`)},
}

var claudeVariantPattern = regexp.MustCompile(`^(?:opus|sonnet|haiku)[- ]?[0-9]+(?:\.[0-9]+)*(?:$|[- ])`)
var entityVersionSuffix = regexp.MustCompile(`^[- ]?v?[0-9]+(?:\.[0-9]+)*(?:$|[- ])`)

func entitySeriesText(value string) string {
	return strings.NewReplacer("‐", "-", "‑", "-", "–", "-", "—", "-", "−", "-").Replace(strings.ToLower(strings.Join(strings.Fields(value), " ")))
}

// Models share a brand across versions and modalities; applications keep their identity.
func canonicalEntityMentionName(name, surface string) string {
	key := entitySeriesText(surface)
	for _, series := range modelSeriesPatterns {
		if series.pattern.MatchString(key) || key == entitySeriesText(series.name) {
			return series.name
		}
	}
	if entitySeriesText(name) == "claude" && claudeVariantPattern.MatchString(key) {
		return "Claude"
	}
	// An unfamiliar series is accepted only when its literal prefix is followed by a version.
	prefix := entitySeriesText(name)
	if prefix != "" && strings.HasPrefix(key, prefix) {
		if entityVersionSuffix.MatchString(strings.TrimPrefix(key, prefix)) {
			return name
		}
	}
	return surface
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
		mention.Name = canonicalEntityMentionName(mention.Name, surface)
		mention.SurfaceName = surface
		key = types.NormalizeEntityKey(mention.Name)
		if utf8.RuneCountInString(mention.Name) > 512 || utf8.RuneCountInString(key) > 512 || strings.ContainsAny(mention.Name, "\r\n\x00") {
			continue
		}
		if seen[key] {
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
