package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestIKB06TemplateEvidenceAndLibraryReview(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	for i, tc := range []struct {
		template, rule, url, publisher, date, identifier, status, reason string
		unverified                                                       bool
	}{
		{types.TemplateExternalTimely, "link_required", "https://example.org/required", "", "", "", "accepted", "", false},
		{types.TemplateExternalTimely, "link_required", "", "Publisher", "2026-10-01", "", "pending_review", "缺少原文链接", false},
		{types.TemplateExternalTimely, "link_or_publisher_date", "https://example.org/optional", "", "", "", "accepted", "", false},
		{types.TemplateExternalTimely, "link_or_publisher_date", "", "Publisher", "2026-10-01", "", "accepted", "", true},
		{types.TemplateExternalTimely, "link_or_publisher_date", "", "", "2026-10-01", "", "pending_review", "缺少原文链接", false},
		{types.TemplateExternalTimely, "link_or_publisher_date", "", "Publisher", "invalid", "", "pending_review", "缺少原文链接", false},
		{types.TemplateExternalDurable, "", "https://example.org/durable", "", "", "", "accepted", "", false},
		{types.TemplateExternalDurable, "", "", "", "", "doi:10.1234/method", "accepted", "", false},
		{types.TemplateExternalDurable, "", "", "", "", "", "pending_review", "缺少原文链接或 identifier", false},
		{types.TemplateOwn, "", "", "", "", "", "accepted", "", false},
		{types.TemplateGenerated, "", "", "", "", "", "accepted", "", false},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			library := &types.Library{ID: fmt.Sprintf("rules-%d", i), SpaceID: h.library.SpaceID, Name: "Rules", Directory: fmt.Sprintf("rules-%d", i), Template: tc.template, ReviewCriteria: fmt.Sprintf("Only accept the unique library criterion number %d", i), SourceEvidenceRule: tc.rule, ModelTiers: h.library.ModelTiers}
			require.NoError(t, h.db.Create(library).Error)
			before := len(capture.reviews)
			req := &types.IngestRequest{Library: library.ID, Title: fmt.Sprintf("Rules %d", i), Content: fmt.Sprintf("Complete original body with distinct observation %d.", i), Acquisition: "verbatim", URL: tc.url, Publisher: tc.publisher, PublishedAt: tc.date, Identifier: tc.identifier}
			result, err := h.service.Ingest(h.ctx, req)
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			require.Equal(t, tc.reason, result.Reason)
			require.Equal(t, tc.unverified, result.SourceUnverified)
			require.Equal(t, tc.unverified, h.article(t, result.EntryID).SourceUnverified)
			if tc.unverified {
				page, err := h.service.ListPagedEntryByLibraryID(h.ctx, library.ID, &types.Pagination{PageSize: 20}, types.EntryListFilter{})
				require.NoError(t, err)
				require.True(t, page.Data.([]*types.Entry)[0].SourceUnverified)
				search := &searchService{db: h.db}
				hits, err := search.mergeEntryHits(h.ctx, h.library.SpaceID, [][]entrySearchHit{{{EntryID: result.EntryID}}}, &types.RetrievalConfig{})
				require.NoError(t, err)
				require.Len(t, hits, 1)
				require.True(t, hits[0].SourceUnverified)
			}
			if tc.status == "accepted" && tc.template != types.TemplateGenerated {
				require.Len(t, capture.reviews, before+1)
				require.Contains(t, capture.reviews[before][0].Content, library.ReviewCriteria)
				require.NotContains(t, capture.reviews[before][0].Content, "Reject unreasoned pure opinion")
			} else {
				require.Len(t, capture.reviews, before)
			}
		})
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
	base := types.IngestRequest{Library: "institutional", URL: "https://example.org/shared", Title: "Shared", Publisher: "Agency", Content: "Complete original body for cross-library isolation.", Acquisition: "verbatim"}
	first := send(base)
	base.Library = "opinions"
	other := send(base)
	require.Equal(t, "accepted", first.Status)
	require.Equal(t, "accepted", other.Status)
	require.NotEqual(t, first.EntryID, other.EntryID)
	require.Equal(t, other.EntryID, send(base).EntryID)
	pending := types.IngestRequest{Library: "opinions", Content: "An unlinked opinion awaiting admission.", Acquisition: "verbatim"}
	awaiting := send(pending)
	require.Equal(t, "pending_review", awaiting.Status)
	require.Equal(t, awaiting.EntryID, send(pending).EntryID)
	// Matching bodies with different URLs stay independent between libraries.
	base.URL = "https://example.org/other-url"
	require.Equal(t, other.EntryID, send(base).EntryID)
	base.Library = "institutional"
	require.Equal(t, first.EntryID, send(base).EntryID)
	// A no-link timely source is also identified by publisher, title and date.
	require.NoError(t, h.db.Model(&types.Library{}).Where("id=?", h.library.ID).Update("source_evidence_rule", "link_or_publisher_date").Error)
	noLink := types.IngestRequest{Library: "institutional", Title: "Bulletin", Publisher: "Agency", PublishedAt: "2026-10-01", Content: "First complete bulletin text.", Acquisition: "verbatim"}
	bulletin := send(noLink)
	require.Equal(t, "accepted", bulletin.Status)
	noLink.Content = "A completely different transcription of the same published bulletin."
	require.Equal(t, bulletin.EntryID, send(noLink).EntryID)
	noLink.PublishedAt = "2026-10-01T08:00:00+08:00"
	require.Equal(t, bulletin.EntryID, send(noLink).EntryID)
	noLink.PublishedAt = "2026-10-02"
	require.NotEqual(t, bulletin.EntryID, send(noLink).EntryID)
	// Durable identifiers match independent of URL and body.
	durable := types.IngestRequest{Library: "references", Identifier: "DOI:10.1234/Exact", Title: "Method", Content: "A complete experimental method.", Acquisition: "verbatim"}
	reference := send(durable)
	durable.Content = "Another supplied edition of the same identified reference."
	require.Equal(t, reference.EntryID, send(durable).EntryID)
	// Own sources compare the latest record first, even when URL/body match an older one.
	own := types.IngestRequest{Library: "mine", SourceRef: "local/notes", URL: "https://example.org/shared", Content: "Own original observation.", Acquisition: "verbatim"}
	original := send(own)
	own.Content = "Own revised observation."
	revised := send(own)
	own.Content = "Own original observation."
	reverted := send(own)
	require.NotEqual(t, original.EntryID, revised.EntryID)
	require.NotEqual(t, original.EntryID, reverted.EntryID)
	require.Equal(t, reverted.EntryID, send(own).EntryID)
	own.SourceRef = "another/source"
	require.NotEqual(t, reverted.EntryID, send(own).EntryID)
	own.SourceRef = ""
	own.Content = "An independent own document without source identity."
	plain := send(own)
	require.Equal(t, plain.EntryID, send(own).EntryID)
	// A confirmed discard blocks all its known keys only in its own library.
	entry := h.article(t, first.EntryID)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, entry, "Confirmed no admission"))
	base.Library = "institutional"
	require.Equal(t, "discarded", send(base).Status)
	base.Library = "opinions"
	require.Equal(t, "exists", send(base).Status)
	checks, err := h.service.CheckURLs(h.ctx, []string{"https://example.org/shared"}, "institutional")
	require.NoError(t, err)
	require.Equal(t, "discarded", checks[0].Status)
	checks, err = h.service.CheckURLs(h.ctx, []string{"https://example.org/shared"}, "opinions")
	require.NoError(t, err)
	require.Equal(t, "in_library", checks[0].Status)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, reference.EntryID), "Discard reference"))
	require.Equal(t, "discarded", send(durable).Status)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, plain.EntryID), "Discard own body"))
	require.Equal(t, "discarded", send(own).Status)
	// source_ref discards preserve the ability to add changed content.
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, reverted.EntryID), "Discard old source content"))
	own.SourceRef = "local/notes"
	own.Content = "New content following a discarded source record."
	require.Equal(t, "accepted", send(own).Status)
	// Generated content always creates a new report and never a discard fingerprint.
	generated := types.IngestRequest{Library: "ai-reports", Title: "Generated report", Content: "Independent generated report.", Acquisition: "model_written"}
	g1, g2 := send(generated), send(generated)
	require.NotEqual(t, g1.EntryID, g2.EntryID)
	require.NoError(t, h.service.rememberIngestDiscard(h.ctx, h.article(t, g1.EntryID), "Delete report"))
	require.Equal(t, "accepted", send(generated).Status)
	// Reposts are recorded for timely libraries only.
	var rows []types.EntryRepostSource
	require.NoError(t, h.db.Find(&rows).Error)
	for _, row := range rows {
		require.Contains(t, []string{entry.LibraryID, h.article(t, other.EntryID).LibraryID}, row.LibraryID)
	}
	require.NotEmpty(t, rows)
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
