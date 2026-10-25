package service

import (
	"reliquary/internal/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIKB07AllHeaderFieldsHaveOneOrder(t *testing.T) {
	h := VaultArticleHeader{
		ID:          "id",
		LibraryID:   "library-id",
		Library:     "示例库",
		Title:       "标题",
		URL:         "https://example.test",
		Publisher:   "同名",
		Author:      "同名",
		PublishedAt: "2026-10-14",
		CollectedAt: "2026-10-14T00:00:00Z",

		Entities: []string{"[[B]]", "[[A]]"},

		Summary:          &types.EntrySummary{Overview: "概述"},
		CollectReason:    "理由",
		Acquisition:      "verbatim",
		SourceRef:        "source",
		Identifier:       "doi:example",
		SourceUnverified: true,
		HasUpdate:        true,
		LatestEntryID:    "latest",
		RelatedEntryIDs:  []string{"b", "a"},
	}
	data := string(RenderVaultArticleHeader(h))
	keys := []string{}
	for _, line := range strings.Split(data, "\n") {
		if line != "" && !strings.HasPrefix(line, " ") && strings.Contains(line, ":") {
			keys = append(keys, strings.SplitN(line, ":", 2)[0])
		}
	}
	require.Equal(
		t,
		[]string{
			"id",
			"library_id",
			"library",
			"title",
			"url",
			"publisher",
			"author",
			"published_at",
			"collected_at",
			"entities",
			"summary",
			"starred",
			"collect_reason",
			"acquisition",
			"source_ref",
			"identifier",
			"source_unverified",
			"has_update",
			"latest_entry_id",
			"related_entry_ids",
		},
		keys,
	)
	require.Contains(t, data, "author: \"同名\"\n")
	minimal := string(RenderVaultArticleHeader(VaultArticleHeader{ID: "id", LibraryID: "library-id", Library: "示例库", Title: "标题", CollectedAt: h.CollectedAt, Summary: &types.EntrySummary{}}))
	require.NotContains(t, minimal, "null")
	require.NotContains(t, minimal, "[]")
	require.NotContains(t, minimal, "summary:")
	require.NotContains(t, minimal, "source_kind:")
	require.Contains(t, minimal, "starred: false\n")
	parsed, _, err := ParseVaultArticle([]byte(minimal))
	require.NoError(t, err)
	require.Empty(t, parsed.Author)
}
