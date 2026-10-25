package service

import (
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIKB09EntityGroupsAcrossLibraries(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	other := types.Library{ID: "other-feed-library", Directory: "media", SpaceID: 1, Name: "人物观点", Template: types.TemplateExternalTimely, SourceEvidenceRule: "link_required", FeedEnabled: true}
	disabled := types.Library{ID: "disabled-feed-library", SpaceID: 1, Name: "参考文献", Template: types.TemplateExternalDurable}
	require.NoError(t, h.db.Create(&other).Error)
	require.NoError(t, h.db.Create(&disabled).Error)
	for _, item := range []struct{ id, library string }{{"one", h.library.ID}, {"two", other.ID}, {"two-similar", other.ID}, {"hidden", disabled.ID}} {
		h.add(t, item.id, "article", "2026-10-01", "event", "semanticlaunch", 0, item.library)
		h.link(t, item.id, true, false)
	}
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
