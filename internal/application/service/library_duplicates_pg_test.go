package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestLibraryDuplicatesKeepEarliestIdenticalBody(t *testing.T) {
	h, _ := newIngestHarness(t)
	ingest := func(library, url, published, content string) string {
		t.Helper()
		result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: library, URL: url, PublishedAt: published, Title: "Orion report", Content: content})
		require.NoError(t, err)
		require.Equal(t, "accepted", result.Status)
		return result.EntryID
	}
	body := "Acme released the Orion model with improved reasoning, fast inference and documented evaluation results."
	later := ingest("official", "https://a.example/orion", "2026-10-20", body)
	earliest := ingest("official", "https://b.example/orion", "2026-10-18", body)
	undated := ingest("official", "https://c.example/orion", "", body)
	ingest("official", "https://d.example/orion", "", body+"\n\nAn analyst added a separate commentary paragraph.")
	ingest("media","https://e.example/orion", "", body)
	otherFirst := ingest("official", "https://f.example/beta", "", "Beta copy without any publication date.")
	otherSecond := ingest("official", "https://g.example/beta", "", "Beta   copy without any\npublication date.")

	groups, err := h.service.ListLibraryDuplicates(h.ctx, h.article(t, later).LibraryID)
	require.NoError(t, err)
	byKeep := map[string][]string{}
	for _, group := range groups {
		for _, item := range group.Remove {
			byKeep[group.Keep.ID] = append(byKeep[group.Keep.ID], item.ID)
		}
	}
	require.Equal(t, map[string][]string{
		earliest:   {later, undated},
		otherFirst: {otherSecond},
	}, byKeep)
	for _, group := range groups {
		if group.Keep.ID == earliest {
			require.Equal(t, "https://b.example/orion", group.Keep.OriginalURL)
		}
	}
}
