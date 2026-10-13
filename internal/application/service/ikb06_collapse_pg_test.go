package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

func TestIKB06SearchListAndFeedFoldOnlyWithinTimelyLibraries(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	owners := map[string]*types.Library{}
	now := time.Now().UTC()
	for i, template := range []string{types.TemplateExternalTimely, types.TemplateExternalTimely, types.TemplateExternalDurable, types.TemplateOwn, types.TemplateGenerated} {
		library := &types.Library{ID: fmt.Sprintf("fold-%d", i), SpaceID: 1, Name: "Fold", Directory: fmt.Sprintf("fold-%d", i), Template: template, FeedEnabled: true}
		if template == types.TemplateExternalTimely {
			library.SourceEvidenceRule = "link_required"
		}
		require.NoError(t, h.db.Create(library).Error)
		for j := 0; j < 2; j++ {
			id := fmt.Sprintf("fold-%d-%d", i, j)
			// Source kind is deliberately identical: template rules decide eligibility.
			h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0, library.ID)
			owners[id] = library
			h.link(t, id, true, false)
			h.collected(t, id, now.Add(time.Duration(-i*2-j-1)*time.Minute))
		}
	}
	check := func(results []*types.EntrySearchResult) {
		require.Len(t, results, 8)
		members := 0
		for _, group := range results {
			for _, member := range group.SimilarReports {
				members++
				require.Equal(t, owners[group.EntryID].ID, owners[member.EntryID].ID)
				require.Equal(t, types.TemplateExternalTimely, owners[group.EntryID].Template)
			}
		}
		require.Equal(t, 2, members)
	}
	check(h.search(t, types.EntrySearchRequest{Query: "searchfixture", Limit: 100}).Results)
	feed := h.feed(t, types.FollowFeedRequest{IncludeSeen: true, Limit: 100})
	require.Len(t, feed.Results, 8)
	for _, group := range feed.Results {
		for _, member := range group.SimilarReports {
			require.Equal(t, owners[group.EntryID].ID, owners[member.EntryID].ID)
			require.Equal(t, types.TemplateExternalTimely, owners[group.EntryID].Template)
		}
	}
	svc := &entryService{db: h.db, repo: repository.NewEntryRepository(h.db)}
	all, err := svc.ListPagedEntryByLibraryID(h.ctx, "", &types.Pagination{Page: 1, PageSize: 100}, types.EntryListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 8, all.Total)
	for _, group := range all.Data.([]*types.Entry) {
		for _, member := range group.SimilarReports {
			require.Equal(t, group.LibraryID, member.LibraryID)
			require.Equal(t, types.TemplateExternalTimely, owners[group.ID].Template)
		}
	}
	first, err := svc.ListPagedEntryByLibraryID(h.ctx, "", &types.Pagination{Page: 1, PageSize: 3}, types.EntryListFilter{})
	require.NoError(t, err)
	second, err := svc.ListPagedEntryByLibraryID(h.ctx, "", &types.Pagination{Page: 2, PageSize: 3}, types.EntryListFilter{})
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, group := range first.Data.([]*types.Entry) {
		seen[group.ID] = true
		for _, member := range group.SimilarReports {
			seen[member.ID] = true
		}
	}
	for _, group := range second.Data.([]*types.Entry) {
		require.False(t, seen[group.ID])
		for _, member := range group.SimilarReports {
			require.False(t, seen[member.ID])
		}
	}
	open := false
	unfolded, err := svc.ListPagedEntryByLibraryID(h.ctx, "", &types.Pagination{PageSize: 100}, types.EntryListFilter{CollapseSimilar: &open})
	require.NoError(t, err)
	require.EqualValues(t, 10, unfolded.Total)
}
