package service

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestSupplementOriginalURLApprovalProcessesAfterModelReview(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "supplemented.md", "# Original research\n\nA complete new paper measures performance and reproducibility."), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "file:///private/file"})
	require.Error(t, err)
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "https://example.org/research"})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, k.ParseStatus)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
	h.drain(t)
	k = h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
	require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages)
	require.False(t, k.SourceUnverified)
}

func TestMissingOriginalURLSkipsModelsAndSupplementRechecks(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "no-link.md", "# No source\n\nOriginal results with observations and measurements."), nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "缺少原文链接", intake.Reason)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Empty(t, h.processor.stages)
	require.Empty(t, k.VaultPath)
	h.processor.reject = true
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "supplement_url", types.SourceInfo{OriginalURL: "https://example.org/confirmed-original"})
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
	require.Len(t, h.queue.tasks, 1, "a rejected supplemented source must not enqueue processing")
	h.processor.reject = false
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "retry", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_supplement_url", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
}

func TestAdmitWithoutSourceForcesFullAndRecordsUserConfirmation(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	body := "# Unlinked instructions\n\nsourceunverifiedfixture steps remain retrievable in full despite the selected tutorial type."
	k, err := h.service.createLegacyManualFixture(h.ctx, h.library.ID, &legacyManualFixturePayload{Title: "Unlinked instructions", Content: body, Status: types.ManualEntryStatusPublish}, types.ChannelWeb)
	require.NoError(t, err)
	h.drain(t)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{})
	require.ErrorContains(t, err, "confirmation")
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{Confirmed: true, OriginalURL: "https://example.org/contradictory-source"})
	require.Error(t, err)
	k, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "admit_unverified", types.SourceInfo{Confirmed: true})
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	require.True(t, k.SourceUnverified)
	require.Equal(t, types.ContentTypeTutorial, k.ContentType)
	require.Equal(t, []string{"article_processing"}, h.processor.stages)
	_, header, storedBody := readVaultArticle(t, vault, k)
	require.NotContains(t, string(header), "\nurl:")
	require.Contains(t, string(storedBody), "sourceunverifiedfixture")
	hits, err := h.service.retrieveEngine.Retrieve(h.ctx, []types.RetrieveParams{{RetrieverType: types.KeywordsRetrieverType, LibraryIDs: []string{h.library.ID}, Query: "sourceunverifiedfixture", TopK: 10}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "review_admit_unverified", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
}

func TestIngestModelRejectReturnsPendingReviewWithReason(t *testing.T) {
	h, vault := newIngestHarness(t)
	h.processor.reject = true
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "institutional", Acquisition: "verbatim", URL: "https://example.org/intake-rejected", Publisher: "Acme", Content: "A complete original selected by an external agent.", AgentSummary: "The collector says this passes review."})
	require.NoError(t, err)
	require.Equal(t, "pending_review", result.Status)
	require.Equal(t, "Fixture sourced article", result.Reason)
	require.Empty(t, h.queue.tasks)
	k := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Empty(t, k.VaultPath)
	entries, err := os.ReadDir(filepath.Join(vault.root, h.library.Directory))
	require.NoError(t, err)
	require.Empty(t, entries)
	require.Equal(t, []string{"intake_review"}, h.processor.stages)
}
