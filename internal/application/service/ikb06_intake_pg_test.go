package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestIKB06AllTemplatesAcceptWithoutSourceOrModelReview(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	for i, template := range []string{types.TemplateExternalTimely, types.TemplateExternalDurable, types.TemplateOwn, types.TemplateGenerated} {
		library := &types.Library{ID: fmt.Sprintf("shared-rule-%d", i), SpaceID: h.library.SpaceID, Name: "Shared rule", Directory: fmt.Sprintf("shared-rule-%d", i), Template: template, ReviewCriteria: "Old custom value gate", ModelTiers: h.library.ModelTiers}
		if template == types.TemplateExternalTimely {
			library.SourceEvidenceRule = "link_required"
		}
		require.NoError(t, h.db.Create(library).Error)
		result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: library.ID, Content: "Unfinished original notes."})
		require.NoError(t, err)
		require.Equal(t, "accepted", result.Status)
		require.Empty(t, result.Reason)
		entry := h.article(t, result.EntryID)
		require.NotEmpty(t, entry.VaultBody)
		require.Empty(t, entry.Acquisition)
		require.Empty(t, capture.reviews)
	}
}

func TestIKB06IdentityAndDiscardStayInLibrary(t *testing.T) {
	h, _ := newIngestHarness(t)
	send := func(request types.IngestRequest) *types.IngestResult {
		t.Helper()
		result, err := h.service.Ingest(h.ctx, &request)
		require.NoError(t, err)
		return result
	}
	base := types.IngestRequest{Library: "official", URL: "https://example.org/shared", Title: "Shared", Publisher: "Agency", Content: "Complete original body for cross-library isolation.", Acquisition: "verbatim"}
	first := send(base)
	base.Library = "media"
	other := send(base)
	require.Equal(t, "accepted", first.Status)
	require.Equal(t, "accepted", other.Status)
	require.NotEqual(t, first.EntryID, other.EntryID)
	require.Equal(t, other.EntryID, send(base).EntryID)
	pending := types.IngestRequest{Library: "media", Content: "An unlinked opinion awaiting admission.", Acquisition: "verbatim"}
	awaiting := send(pending)
	require.Equal(t, "accepted", awaiting.Status)
	require.NotEqual(t, awaiting.EntryID, send(pending).EntryID)
	// Matching bodies with different URLs stay independent between libraries.
	base.URL = "https://example.org/other-url"
	require.NotEqual(t, other.EntryID, send(base).EntryID)
	base.Library = "official"
	require.NotEqual(t, first.EntryID, send(base).EntryID)
	// Publisher, title and date alone cannot identify a source.
	require.NoError(t, h.db.Model(&types.Library{}).Where("id=?", h.library.ID).Update("source_evidence_rule", "link_or_publisher_date").Error)
	noLink := types.IngestRequest{Library: "official", Title: "Bulletin", Publisher: "Agency", PublishedAt: "2026-10-01", Content: "First complete bulletin text.", Acquisition: "verbatim"}
	bulletin := send(noLink)
	require.Equal(t, "accepted", bulletin.Status)
	noLink.Content = "A completely different transcription of the same published bulletin."
	require.NotEqual(t, bulletin.EntryID, send(noLink).EntryID)
	noLink.PublishedAt = "2026-10-01T08:00:00+08:00"
	require.NotEqual(t, bulletin.EntryID, send(noLink).EntryID)
	noLink.PublishedAt = "2026-10-02"
	require.NotEqual(t, bulletin.EntryID, send(noLink).EntryID)
	// Durable identifiers match independent of URL and body.
	durable := types.IngestRequest{Library: "references", Identifier: "DOI:10.1234/Exact", Title: "Method", Content: "A complete experimental method.", Acquisition: "verbatim"}
	reference := send(durable)
	durable.Content = "Another supplied edition of the same identified reference."
	require.Equal(t, reference.EntryID, send(durable).EntryID)
	// Own sources compare the latest saved content, including a reverted body.
	own := types.IngestRequest{Library: "personal", SourceRef: "local/notes", Content: "Own original observation.", Acquisition: "verbatim"}
	original := send(own)
	own.Content = "Own revised observation."
	revised := send(own)
	own.Content = "Own original observation."
	reverted := send(own)
	require.Equal(t, original.EntryID, revised.EntryID)
	require.Equal(t, original.EntryID, reverted.EntryID)
	require.Equal(t, reverted.EntryID, send(own).EntryID)
	own.SourceRef, own.URL = "another/source", ""
	require.NotEqual(t, reverted.EntryID, send(own).EntryID)
	own.SourceRef = ""
	own.Content = "An independent own document without source identity."
	plain := send(own)
	require.NotEqual(t, plain.EntryID, send(own).EntryID)
	// A confirmed discard blocks all its known keys only in its own library.
	entry := h.article(t, first.EntryID)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, entry, "Confirmed no admission"))
	base.Library, base.URL = "official", "https://example.org/shared"
	require.Equal(t, "discarded", send(base).Status)
	base.Library = "media"
	require.Equal(t, "exists", send(base).Status)
	checks, err := h.service.CheckURLs(h.ctx, []string{"https://example.org/shared"}, "official")
	require.NoError(t, err)
	require.Equal(t, "discarded", checks[0].Status)
	checks, err = h.service.CheckURLs(h.ctx, []string{"https://example.org/shared"}, "media")
	require.NoError(t, err)
	require.Equal(t, "in_library", checks[0].Status)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, reference.EntryID), "Discard reference"))
	require.Equal(t, "discarded", send(durable).Status)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, plain.EntryID), "Discard own body"))
	require.Equal(t, "discarded", send(own).Status)
	// source_ref discards preserve the ability to add changed content.
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, reverted.EntryID), "Discard old source content"))
	own.SourceRef = "local/notes"
	own.URL = ""
	own.Content = "New content following a discarded source record."
	require.Equal(t, "accepted", send(own).Status)
	// Generated content has the same source identity and explicit-discard rules.
	generated := types.IngestRequest{Library: "ai-output", Title: "Generated report", Content: "Independent generated report.", Acquisition: "model_written"}
	g1, g2 := send(generated), send(generated)
	require.NotEqual(t, g1.EntryID, g2.EntryID)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, g1.EntryID), "Delete report"))
	require.Equal(t, "discarded", send(generated).Status)
	// Ingestion no longer creates repost records.
	var rows []types.EntryRepostSource
	require.NoError(t, h.db.Find(&rows).Error)
	for _, row := range rows {
		require.Contains(t, []string{entry.LibraryID, h.article(t, other.EntryID).LibraryID}, row.LibraryID)
	}
	require.Empty(t, rows)
}

func TestIKB06DurableHasNoNearDuplicateOrRepost(t *testing.T) {
	h, _ := newIngestHarness(t)
	body := "Acme released the Orion model with improved reasoning, fast inference, multimodal input, and a context window of one million tokens. The report includes benchmark results, evaluation methodology, pricing, and deployment requirements."
	first, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "references", Identifier: "doi:10.1000/one", Content: body, Acquisition: "verbatim"})
	require.NoError(t, err)
	second, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "references", Identifier: "doi:10.1000/two", Content: strings.Replace(body, "one million", "two million", 1), Acquisition: "verbatim"})
	require.NoError(t, err)
	require.NotEqual(t, first.EntryID, second.EntryID)
	var count int64
	require.NoError(t, h.db.Model(&types.EntryRepostSource{}).Count(&count).Error)
	require.Zero(t, count)
}
