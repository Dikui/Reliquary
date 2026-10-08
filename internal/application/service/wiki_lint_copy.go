package service

import (
	"context"
	"fmt"
	"strings"

	"reliquary/internal/types"
)

// Format descriptions when reading, using the same language context as lint.
func wikiSourceModifiedDescription(ctx context.Context, title string, count int) string {
	if strings.HasPrefix(strings.ToLower(types.LanguageFromContextOrDefault(ctx)), "zh") {
		return fmt.Sprintf("页面“%s”的 %d 个来源分块已修改", title, count)
	}
	noun := "chunks"
	if count == 1 {
		noun = "chunk"
	}
	return fmt.Sprintf("Page '%s' has %d modified source %s", title, count, noun)
}
