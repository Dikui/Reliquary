package service

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestIngestLibrarySourceIdentity(t *testing.T) {
	h, _ := newIngestHarness(t)
	req := types.IngestRequest{Library: "personal", SourceRef: "work/measurements", Content: "# Original\n\nMeasured pressure equals 42.", Acquisition: "verbatim", Title: "Measurements"}
	on := true
	req.Graph = &on
	first, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.True(t, *first.Graph)
	again, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, again.EntryID)
	require.Equal(t, "exists", again.Status)
	req.Content = "Measured pressure equals 43."
	second, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.Equal(t, first.EntryID, second.EntryID)
	req.Content = "Measured pressure equals 44."
	third, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	for _, id := range []string{first.EntryID, second.EntryID, third.EntryID} {
		entry := h.article(t, id)
		require.Equal(t, third.EntryID, entry.LatestEntryID)
		require.False(t, entry.HasUpdate)
	}
	// Reverting also replaces the same record.
	req.Content = "Measured pressure equals 43."
	fourth, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.Equal(t, second.EntryID, fourth.EntryID)
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := req
			r, e := h.service.Ingest(h.ctx, &copy)
			errs[i] = e
			if r != nil {
				ids[i] = r.EntryID
			}
		}(i)
	}
	wg.Wait()
	for i := range ids {
		require.NoError(t, errs[i])
		require.Equal(t, fourth.EntryID, ids[i])
	}
	require.True(t, *h.article(t, first.EntryID).Graph)
	// Serialize first-ever concurrent submissions too, when no matching row exists.
	fresh := req
	fresh.SourceRef = "fresh-concurrent"
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := fresh
			r, e := h.service.Ingest(h.ctx, &copy)
			errs[i] = e
			if r != nil {
				ids[i] = r.EntryID
			}
		}(i)
	}
	wg.Wait()
	for i := range ids {
		require.NoError(t, errs[i])
		require.Equal(t, ids[0], ids[i])
	}
	longRef := req
	longRef.SourceRef = strings.Repeat("界", types.MaxIngestSourceRefRunes)
	longResult, err := h.service.Ingest(h.ctx, &longRef)
	require.NoError(t, err)
	require.NotEmpty(t, longResult.EntryID)
	req.SourceRef = ""
	a, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	b, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.NotEqual(t, a.EntryID, b.EntryID)
	custom := &types.Library{ID: "own-library", SpaceID: h.library.SpaceID, Name: "Other own", Directory: "other-own", Template: types.TemplateOwn, ModelTiers: h.library.ModelTiers}
	require.NoError(t, h.db.Create(custom).Error)
	req.Library, req.SourceRef = "other-own", "work/measurements"
	independent, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.NotEqual(t, fourth.EntryID, independent.EntryID)
	req.Library = "official"
	_, err = h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	req.SourceRef = ""
	req.Identifier = "DOI: 10.1234/AbC "
	_, err = h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	req.Library = "references"
	req.URL = "https://example.org/identifier"
	req.Publisher = "Journal"
	reference, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.Equal(t, "doi:10.1234/abc", reference.Identifier)
	req.Identifier = "isbn: AbC"
	require.NoError(t, validateLibraryIngest(&req, &types.Library{Template: types.TemplateExternalDurable}))
	require.Equal(t, "isbn:AbC", req.Identifier)
	req.Identifier = "invalid"
	require.Error(t, validateLibraryIngest(&req, &types.Library{Template: types.TemplateExternalDurable}))
	req.Identifier = ""
	req.SourceRef = strings.Repeat("界", 2049)
	req.Library = "personal"
	_, err = h.service.Ingest(h.ctx, &req)
	require.ErrorContains(t, err, "2048")

	// Read method projects the same latest identity used by HTTP entry details.
	read, err := h.service.ReadEntry(h.ctx, first.EntryID, 0, 100)
	require.NoError(t, err)
	require.False(t, read.HasUpdate)
	require.Equal(t, fourth.EntryID, read.LatestEntryID)
	search := &searchService{db: h.db}
	hits, err := search.mergeEntryHits(h.ctx, h.library.SpaceID, [][]entrySearchHit{{{EntryID: first.EntryID}}}, &types.RetrievalConfig{})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.False(t, hits[0].HasUpdate)
	require.Equal(t, fourth.EntryID, hits[0].LatestEntryID)
	require.True(t, *hits[0].Graph)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source_ref=? AND library_id=?", "work/measurements", h.article(t, first.EntryID).LibraryID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestIngestFiveMillionCodePoints(t *testing.T) {
	h, _ := newIngestHarness(t)
	req := types.IngestRequest{Library: "personal", Title: "Long record", Content: strings.Repeat("界", types.MaxIngestContentRunes), Acquisition: "verbatim"}
	result, err := h.service.Ingest(h.ctx, &req)
	require.NoError(t, err)
	require.NotEmpty(t, result.EntryID)
	req.Content += "界"
	_, err = h.service.Ingest(h.ctx, &req)
	require.ErrorContains(t, err, "5000000")
}
