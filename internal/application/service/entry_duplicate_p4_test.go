package service

import (
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"strings"
	"testing"
)

func TestP4MessageURLNormalization(t *testing.T) {
	for _, raw := range []string{
		"https://EXAMPLE.COM/Article/", "https://example.com/article", "https://example.com/article/?utm_source=rss&utm_campaign=daily#section",
		"https://example.com:443/ARTICLE?fbclid=one&gclid=two&mc_cid=three",
	} {
		require.Equal(t, "https://example.com/article", normalizeMessageURL(raw), raw)
	}
	require.Equal(t, "https://example.com/article?id=ABC", normalizeMessageURL("https://example.com/article/?utm_medium=social&id=ABC"))
	require.NotEqual(t, normalizeMessageURL("https://example.com/article?id=1"), normalizeMessageURL("https://example.com/article?id=2"))
	require.Empty(t, normalizeMessageURL("not a URL"))
}

func TestP4RepostNamePreservesFullWidthCharacters(t *testing.T) {
	require.Equal(t, "acme news", normalizeRepostName("  ACME\t News  "))
	require.Equal(t, "ａｃｍｅ", normalizeRepostName(" ＡＣＭＥ "))
	require.NotEqual(t, normalizeRepostName("ＡＣＭＥ"), normalizeRepostName("ACME"))
}

func TestP4SameOrSimilarBodyWithoutSourceRemainsSeparate(t *testing.T) {
	h, _ := newIngestHarness(t)
	request := &types.IngestRequest{Library: "official", Title: "Orion report", Content: "Acme released the Orion model with improved reasoning, fast inference and documented evaluation results."}
	first, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	same, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	request.Content = strings.Replace(request.Content, "reasoning,", "reasoning;", 1)
	similar, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.NotEqual(t, first.EntryID, same.EntryID)
	require.NotEqual(t, first.EntryID, similar.EntryID)
	require.Equal(t, "accepted", similar.Status)
	require.Len(t, h.queue.tasks, 3)
	var count int64
	require.NoError(t, h.db.Model(&types.EntryRepostSource{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestP4SourceMatchMustBeUnambiguous(t *testing.T) {
	h, _ := newIngestHarness(t)
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", SourceRef: "source-a", Content: "First original measurements."})
	require.NoError(t, err)
	second, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", Identifier: "doi:10.1234/b", Content: "Second original measurements."})
	require.NoError(t, err)
	result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{{Library: "official", SourceRef: "source-a", Identifier: "doi:10.1234/b", Content: "This ambiguous update must not replace either source."}}})
	require.NoError(t, err)
	require.Equal(t, "invalid", result.Items[0].Status)
	require.Contains(t, result.Items[0].Reason, "multiple entries")
	require.Contains(t, h.article(t, first.EntryID).VaultBody, "First original")
	require.Contains(t, h.article(t, second.EntryID).VaultBody, "Second original")
	require.Len(t, h.queue.tasks, 2)
}
