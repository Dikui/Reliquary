package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

func TestLibraryPromptsUseOverridesAndKeepProtocols(t *testing.T) {
	library := &types.Library{PromptOverrides: map[string]string{
		"article_processing": "本库理解规则", "library_summary": "本库摘要要求",
		"article_entities": "本库实体规则", "article_relations": "本库关系规则",
		"summary_merge": "本库合并规则", "topic_backfill": "本库主题规则",
	}}
	require.NoError(t, types.ValidateLibraryPromptOverrides(library.PromptOverrides))
	prompt := libraryProcessingPrompt(library, &types.Entry{})
	for _, id := range types.LibraryPromptIDs[:4] {
		require.Contains(t, prompt, library.PromptOverrides[id])
		require.NotContains(t, libraryProcessingPrompt(&types.Library{}, &types.Entry{}), library.PromptOverrides[id])
	}
	require.Contains(t, prompt, `"content_type"`)
	require.Contains(t, prompt, "content_types=")
	require.Contains(t, segmentSummaryPrompt(library), "本库摘要要求")
	require.Contains(t, segmentSummaryPrompt(library), "本库合并规则")
	require.Contains(t, segmentSummaryPrompt(library), `"summary"`)
	require.Contains(t, topicBackfillPrompt(library), "本库主题规则")
	require.Contains(t, topicBackfillPrompt(library), `"decisions"`)
	delete(library.PromptOverrides, "library_summary")
	require.Equal(t, config.Prompt("library_summary"), libraryPrompt(library, "library_summary"))
	require.Len(t, config.LibraryPromptDefaults(), len(types.LibraryPromptIDs))
	require.Error(t, types.ValidateLibraryPromptOverrides(map[string]string{"graph_verification": "不可覆盖核查"}))
	require.Error(t, types.ValidateLibraryPromptOverrides(map[string]string{"article_processing": " \n"}))
	require.Error(t, types.ValidateLibraryPromptOverrides(map[string]string{"article_processing": strings.Repeat("字", types.MaxLibraryPromptChars+1)}))
}
