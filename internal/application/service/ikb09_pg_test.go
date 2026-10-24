package service

import (
	"reliquary/internal/types"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIKB09FeedLibrariesWatermarksAndEntityGroups(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	other := types.Library{ID: "other-feed-library", Directory: "media", SpaceID: 1, Name: "人物观点", Template: types.TemplateExternalTimely, SourceEvidenceRule: "link_required", FeedEnabled: true}
	disabled := types.Library{ID: "disabled-feed-library", SpaceID: 1, Name: "参考文献", Template: types.TemplateExternalDurable}
	require.NoError(t, h.db.Create(&other).Error)
	require.NoError(t, h.db.Create(&disabled).Error)
	now, err := h.follows.feedNow(h.ctx)
	require.NoError(t, err)
	for _, item := range []struct{ id, library string }{{"one", h.library.ID}, {"two", other.ID}, {"two-similar", other.ID}, {"hidden", disabled.ID}} {
		h.add(t, item.id, "article", "2026-10-01", "event", "semanticlaunch", 0, item.library)
		h.collected(t, item.id, now.Add(-time.Minute))
		h.link(t, item.id, true, false)
	}
	state, err := h.follows.State(h.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, state.NewCount)
	require.Len(t, state.Libraries, 2)
	folded := h.feed(t, types.FollowFeedRequest{IncludeSeen: true})
	require.Len(t, folded.Results, 3, "each matching entry is returned independently")
	only := h.feed(t, types.FollowFeedRequest{Libraries: []string{other.Directory}, IncludeSeen: true})
	require.Len(t, only.Results, 2)
	require.Equal(t, other.ID, only.Results[0].LibraryID)
	_, err = h.follows.Feed(h.ctx, types.FollowFeedRequest{Libraries: []string{"unknown"}})
	require.Error(t, err)
	state, err = h.follows.MarkViewed(h.ctx, &only.ViewedAt, other.ID)
	require.NoError(t, err)
	require.Zero(t, state.NewCount)
	global, err := h.follows.State(h.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, global.NewCount)
	fresh := h.feed(t, types.FollowFeedRequest{})
	require.Equal(t, []string{"one"}, followFeedIDs(fresh))
	state, err = h.follows.MarkViewed(h.ctx, &fresh.ViewedAt)
	require.NoError(t, err)
	require.Zero(t, state.NewCount)
	for _, lib := range state.Libraries {
		require.Equal(t, fresh.ViewedAt, *lib.LastViewedAt)
	}
	var n int64
	require.NoError(t, h.db.Table("follow_feed_library_states").Where("library_id=?", disabled.ID).Count(&n).Error)
	require.Zero(t, n)
	entity, err := NewEntityService(h.db, nil).EntityDetail(h.ctx, "entity-follow")
	require.NoError(t, err)
	require.Len(t, entity.LibraryGroups, 3, "entity identity and related material span enabled and disabled libraries")
	counts := map[string]int{}
	for _, group := range entity.LibraryGroups {
		counts[group.LibraryID] = group.Count
		require.NotEmpty(t, group.LibraryName)
	}
	require.Equal(t, map[string]int{h.library.ID: 1, other.ID: 2, disabled.ID: 1}, counts)
}

func TestIKB09FeedUsesDatabaseClockAndPreservesArrivals(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	opening := h.feed(t, types.FollowFeedRequest{IncludeSeen: true})
	h.add(t, "after-open-clock", "article", "2026-10-01", "event", "semanticlaunch", 0)
	h.link(t, "after-open-clock", true, false)
	var ready time.Time
	require.NoError(t, h.db.Table("entries").Select("follow_ready_at").Where("id=?", "after-open-clock").Scan(&ready).Error)
	state, err := h.follows.MarkViewed(h.ctx, &opening.ViewedAt)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.NewCount)
	feed := h.feed(t, types.FollowFeedRequest{})
	require.Len(t, feed.Results, 1)
	require.False(t, feed.ViewedAt.Before(ready))
	t.Logf("host clock %s; database ready %s; database cutoff %s", time.Now().UTC().Format(time.RFC3339Nano), ready.Format(time.RFC3339Nano), feed.ViewedAt.Format(time.RFC3339Nano))
}
