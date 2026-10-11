package service

import (
	"strings"

	"reliquary/internal/types"
)

// buildKnowledgeIndexContent supplies vector context without changing stored
// chunk bodies or the full-text index. Custom metadata is not vector context.
func buildKnowledgeIndexContent(knowledge *types.Knowledge, content string) string {
	if knowledge == nil {
		return content
	}
	parts := []string{}
	if title := vectorPrefixPart(knowledge.Title); title != "" {
		parts = append(parts, title)
	}
	switch knowledge.SourceKind {
	case "mine":
		parts = append(parts, "本人")
	case "report":
		parts = append(parts, "整理结果")
	default:
		if source, err := articleSourceInfo(knowledge); err == nil {
			if publisher := vectorPrefixPart(source.Publisher); publisher != "" {
				parts = append(parts, publisher)
			}
			if date := vectorPrefixPart(source.PublishedAt); date != "" {
				parts = append(parts, date)
			}
		}
	}
	if len(parts) == 0 {
		return content
	}
	return strings.Join(parts, " · ") + "\n" + content
}

func vectorPrefixPart(value string) string { return strings.Join(strings.Fields(value), " ") }
