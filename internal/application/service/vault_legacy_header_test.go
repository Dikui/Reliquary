package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntryContractLegacyHeaderConversionRequiresReplicaVersion(t *testing.T) {
	body := []byte("# Body\n\n原始正文，包含 related_article_ids 字面文本。\n")
	header := RenderVaultArticleHeader(VaultArticleHeader{ID: "entry", LibraryID: "library", Title: "Title", CollectedAt: "2026-10-14T00:00:00Z", LatestEntryID: "latest", RelatedEntryIDs: []string{"source"}})
	old := strings.ReplaceAll(strings.ReplaceAll(string(header), "latest_entry_id", "latest_article_id"), "related_entry_ids", "related_article_ids")
	data := append([]byte(old), body...)
	_, _, err := ParseVaultArticle(data)
	require.Error(t, err)
	h, actual, err := parseVaultReplicaHeader(data, true)
	require.NoError(t, err)
	require.Equal(t, body, actual)
	require.Equal(t, "latest", h.LatestEntryID)
	require.Equal(t, []string{"source"}, h.RelatedEntryIDs)
	current := string(RenderVaultArticleHeader(h))
	require.Contains(t, current, "latest_entry_id:")
	require.Contains(t, current, "related_entry_ids:")
	require.NotContains(t, current, "latest_article_id:")
	require.NotContains(t, current, "related_article_ids:")
	_, _, err = parseVaultReplicaHeader(data, false)
	require.Error(t, err)
	conflict := []byte(strings.Replace(old, "latest_article_id:", "latest_entry_id: \"conflict\"\nlatest_article_id:", 1))
	_, _, err = parseVaultReplicaHeader(append(conflict, body...), true)
	require.ErrorContains(t, err, "duplicate")
}
