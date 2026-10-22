package service

import (
	"encoding/json"
	"testing"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOriginalSourceExtractionDoesNotTreatOrdinaryLinksAsSource(t *testing.T) {
	require.Empty(t, extractOriginalSourceLink("# Notes\n\nA related page is https://example.org/reference.\n\n[Reference](https://example.org/another)", nil))
	require.Empty(t, extractOriginalSourceLink(`<link rel="canonical" href="file:///etc/passwd">`, nil))
	for _, ordinary := range []string{"Resources: https://example.org/tool", "This resource is https://example.org/tool", "Source reference https://example.org/tool", "A paper discusses 2601.12345."} {
		require.Empty(t, extractOriginalSourceLink(ordinary, nil), ordinary)
	}
	require.Equal(t, "https://example.org/paper(title)", extractOriginalSourceLink("Source: https://example.org/paper(title)", nil))
	require.Equal(t, "https://example.org/paper(title)", extractOriginalSourceLink("原文链接：[文章](https://example.org/paper(title))", nil))
	require.Equal(t, "https://arxiv.org/abs/2601.12345v2", extractOriginalSourceLink("A paper.", map[string]string{"arxiv": "2601.12345v2"}))
	require.Equal(t, "https://arxiv.org/abs/cs/9901001", extractOriginalSourceLink("arXiv:cs/9901001", nil))
	require.Equal(t, "https://doi.org/10.1234/example", extractOriginalSourceLink("(DOI: 10.1234/example)", nil))
	confirmation, err := json.Marshal(types.SourceInfo{Confirmed: true})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(confirmation))
}

func TestMissingSourceAndOldCriteriaDoNotGateIngest(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	require.NoError(t, h.db.Model(h.library).Updates(map[string]any{
		"source_evidence_rule": "link_required", "review_criteria": "Only accept complete original publications.",
	}).Error)
	h.processor.reject = true
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", Content: "An unfinished note records 42 pascals."})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Empty(t, capture.reviews)
	require.Empty(t, result.Reason)
	h.drain(t)
	entry := h.article(t, result.EntryID)
	intake, err := entry.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, types.ParseStatusCompleted, entry.ParseStatus)
	require.Empty(t, intake.SourceInfo.OriginalURL)
	require.Empty(t, capture.reviews)
	require.NotEmpty(t, entry.VaultBody)
}
