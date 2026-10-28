package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

func TestIKB06SearchListAndFeedFoldWithinEveryLibrary(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	owners := map[string]*types.Library{}
	now := time.Now().UTC()
	for i := range 5 {
		library := &types.Library{ID: fmt.Sprintf("fold-%d", i), SpaceID: 1, Name: "Fold", Directory: fmt.Sprintf("fold-%d", i), Template: types.TemplateStandard, FeedEnabled: true}

		require.NoError(t, h.db.Create(library).Error)
		entity := types.Entity{ID: "fold-entity-" + library.ID, SpaceID: 1, LibraryID: library.ID, CanonicalName: "Followed company", NormalizedName: "followed company", EntityType: "company"}
		require.NoError(t, h.db.Create(&entity).Error)
		_, err := h.follows.Follow(h.ctx, "entity", entity.ID)
		require.NoError(t, err)
		for j := 0; j < 2; j++ {
			id := fmt.Sprintf("fold-%d-%d", i, j)
			// Each library folds independently of its role or content catalog.
			h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0, library.ID)
			owners[id] = library
			require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: id, EntityID: entity.ID}).Error)
			h.collected(t, id, now.Add(time.Duration(-i*2-j-1)*time.Minute))
		}
	}
	check := func(results []*types.EntrySearchResult) {
		require.Len(t, results, 5)
		members := 0
		for _, group := range results {
			for _, member := range group.SimilarReports {
				members++
				require.Equal(t, owners[group.EntryID].ID, owners[member.EntryID].ID)
				require.Equal(t, types.TemplateStandard, owners[group.EntryID].Template)
			}
		}
		require.Equal(t, 5, members)
	}
	check(h.search(t, types.EntrySearchRequest{Query: "searchfixture", Limit: 100}).Results)
	feed := h.feed(t, types.FollowFeedRequest{IncludeSeen: true, Limit: 100})
	require.Len(t, feed.Results, 5)
	for _, group := range feed.Results {
		for _, member := range group.SimilarReports {
			require.Equal(t, owners[group.EntryID].ID, owners[member.EntryID].ID)
			require.Equal(t, types.TemplateStandard, owners[group.EntryID].Template)
		}
	}
	svc := &entryService{db: h.db, repo: repository.NewEntryRepository(h.db)}
	all, err := svc.ListPagedEntryByLibraryID(h.ctx, "", &types.Pagination{Page: 1, PageSize: 100}, types.EntryListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 5, all.Total)
	for _, group := range all.Data.([]*types.Entry) {
		for _, member := range group.SimilarReports {
			require.Equal(t, group.LibraryID, member.LibraryID)
			require.Equal(t, types.TemplateStandard, owners[group.ID].Template)
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
