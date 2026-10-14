package router

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"testing"
	"time"
)

func TestIKB09MCPFollowFeedLibrariesAndTopicWrites(t *testing.T) {
	f := newMCPFixture(t)
	entity, _ := mcpFollowTargets(t, f)
	client := f.connect(t, "2025-11-25")
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": entity.ID}, nil)
	other := types.Library{ID: uuid.NewString(), Directory: "ikb09-second", Name: "Second feed", SpaceID: 1, Template: types.TemplateExternalTimely, SourceEvidenceRule:"link_required", FeedEnabled: true}
	require.NoError(t, f.db.Create(&other).Error)
	first := mcpFollowArticle(t, f, "First feed", time.Now().Add(-time.Hour), entity.ID, "")
	second := types.Entry{ID: uuid.NewString(), LibraryID: other.ID, SpaceID: 1, Title: "Second feed entry", Type: "manual", SourceKind: "article", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, f.db.Create(&second).Error)
	require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: second.ID, EntityID: entity.ID}).Error)
	var feed types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"libraries": []string{f.library.ID}, "collapse_similar": false}, &feed)
	require.Equal(t, []string{first.ID}, mcpFeedIDs(&feed))
	mcpCall(t, client, "follow_feed", map[string]any{"libraries": []string{f.library.ID, other.Directory}, "collapse_similar": false}, &feed)
	require.ElementsMatch(t, []string{first.ID, second.ID}, mcpFeedIDs(&feed))
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &feed)
	require.ElementsMatch(t, []string{first.ID, second.ID}, mcpFeedIDs(&feed))
	var n int64
	require.NoError(t, f.db.Table("follow_feed_library_states").Count(&n).Error)
	require.Zero(t, n, "MCP never acknowledges a view")
	var topic types.Topic
	mcpCall(t, client, "create_topic", map[string]any{"name": "MCP scope", "library_ids": []string{other.ID}}, &topic)
	require.Equal(t, []string{other.ID}, topic.LibraryIDs)
	mcpCall(t, client, "update_topic", map[string]any{"id": topic.ID, "library_ids": []string{}}, &topic)
	require.Empty(t, topic.LibraryIDs)
	// HTTP uses the same selected scope for opening and marking all viewed.
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	response := personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/view", `{"libraries":["`+other.ID+`"]}`, f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	state, err := service.NewFollowService(f.db).State(f.ctx)
	require.NoError(t, err)
	for _, library := range state.Libraries {
		if library.LibraryID == other.ID {
			require.NotNil(t, library.LastViewedAt)
		} else {
			require.Nil(t, library.LastViewedAt)
		}
	}
}
