package tools

import (
	"regexp"
	"strings"
	"testing"

	"reliquary/internal/types"
)

func TestExtractChunkMatchSnippet_DocumentUsesBodyContext(t *testing.T) {
	content := strings.Repeat("前置。", 50) + "TARGET" + strings.Repeat("后续。", 50)
	chunk := &types.Chunk{ChunkType: types.ChunkTypeText, Content: content}
	re := regexp.MustCompile(`TARGET`)
	snippet := extractChunkMatchSnippet(chunk, []*regexp.Regexp{re})

	if !strings.Contains(snippet, "TARGET") {
		t.Fatalf("missing match: %s", snippet)
	}
	if !strings.Contains(snippet, "前置") || !strings.Contains(snippet, "后续") {
		t.Fatalf("expected expanded body context: %s", snippet)
	}
}
