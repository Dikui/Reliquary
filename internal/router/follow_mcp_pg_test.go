package router

import (
	"context"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func mcpFollowTargets(t *testing.T, f *mcpFixture) types.Entity {
	t.Helper()
	entity := types.Entity{
		ID:             uuid.NewString(),
		SpaceID:        types.SingleSpaceID,
		CanonicalName:  "Follow Acme",
		NormalizedName: "follow acme",
	}
	require.NoError(t, f.db.Create(&entity).Error)
	return entity
}

func mcpFollowArticle(t *testing.T, f *mcpFixture, title string, collected time.Time, entityID, topicID string) *types.Entry {
	t.Helper()
	article := f.seedArticle(t, title, "full", "# "+title+"\n\nOriginal follow evidence.\n")
	require.NoError(t, f.db.Model(article).Updates(map[string]any{"created_at": collected, "follow_ready_at": collected}).Error)
	if entityID != "" {
		require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: types.SingleSpaceID, EntryID: article.ID, EntityID: entityID, CreatedAt: collected}).Error)
	}
	story := types.Story{ID: article.ID, SpaceID: 1, Title: title, FirstFactAt: collected, LastFactAt: collected}
	require.NoError(t, f.db.Create(&story).Error)
	fact := types.Fact{
		ID: article.ID, SpaceID: 1, StoryID: story.ID, Title: title,
		Subject: "Acme", Action: "released", CreatedAt: collected,
	}
	if entityID != "" {
		fact.SubjectEntityID = &entityID
	}
	require.NoError(t, f.db.Create(&fact).Error)
	require.NoError(t, f.db.Create(&types.EntryFact{
		SpaceID: 1, EntryID: article.ID, FactID: fact.ID, Role: "source",
		Quote: "Original follow evidence.", CreatedAt: collected,
	}).Error)
	return article
}

func mcpFeedIDs(result *types.FollowFeedResponse) []string {
	ids := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		ids = append(ids, item.Story.ID)
	}
	return ids
}

func mcpMatchedIDs(item types.FollowFeedCard) []string {
	ids := make([]string, 0, len(item.MatchedFollows))
	for _, target := range item.MatchedFollows {
		ids = append(ids, target.TargetID)
	}
	return ids
}

func TestMCPFollowToolsValidateArgumentsWithoutWrites(t *testing.T) {
	f := newMCPFixture(t)
	client := f.connect(t, "2025-11-25")
	for _, invalid := range []struct {
		tool string
		args map[string]any
	}{
		{"follow", map[string]any{"target_type": "article", "target_id": uuid.NewString()}},
		{"follow", map[string]any{"target_type": "entity"}},
		{"unfollow", map[string]any{"target_type": "topic"}},
		{"follow_feed", map[string]any{"since": "invalid date"}},
		{"follow_feed", map[string]any{"limit": 0}},
		{"follow_feed", map[string]any{"limit": 101}},
		{"follow_feed", map[string]any{"include_seen": true}},
		{"list_follows", map[string]any{"unknown": true}},
	} {
		response, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: invalid.tool, Arguments: invalid.args})
		require.NoError(t, err)
		require.True(t, response.IsError, "%s %v", invalid.tool, invalid.args)
	}
	var count int64
	require.NoError(t, f.db.Model(&types.OperationLog{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMCPFollowMergedEntityIDResolvesToRetainedTarget(t *testing.T) {
	f := newMCPFixture(t)
	merged := mcpFollowTargets(t, f)
	retained := types.Entity{
		ID:             uuid.NewString(),
		SpaceID:        types.SingleSpaceID,
		CanonicalName:  "Retained Acme",
		NormalizedName: "retained acme",
	}
	require.NoError(t, f.db.Create(&retained).Error)
	client := f.connect(t, "2025-11-25")
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": merged.ID}, nil)
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": retained.ID}, nil)
	article := mcpFollowArticle(t, f, "Merged target article", time.Now().UTC().Add(-time.Hour), merged.ID, "")
	require.NoError(t, service.NewEntityService(f.db, nil).MergeEntities(f.ctx, retained.ID, merged.ID))
	var target types.FollowTarget
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": merged.ID}, &target)
	require.Equal(t, retained.ID, target.TargetID)
	require.Equal(t, retained.CanonicalName, target.Name)
	var listed struct {
		Follows []types.FollowTarget `json:"follows"`
	}
	mcpCall(t, client, "list_follows", map[string]any{}, &listed)
	require.Len(t, listed.Follows, 1)
	require.Equal(t, retained.ID, listed.Follows[0].TargetID)
	var feed types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{}, &feed)
	require.Equal(t, []string{article.ID}, mcpFeedIDs(&feed))
	require.Equal(t, []string{retained.ID}, mcpMatchedIDs(feed.Results[0]))
	mcpCall(t, client, "unfollow", map[string]any{"target_type": "entity", "target_id": merged.ID}, nil)
	mcpCall(t, client, "list_follows", map[string]any{}, &listed)
	require.Empty(t, listed.Follows)
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("operation_type IN ?", []string{"follow", "unfollow"}).Find(&logs).Error)
	require.Len(t, logs, 3)
	for _, entry := range logs {
		if entry.OperationType == "unfollow" {
			require.Equal(t, retained.ID, entry.ObjectID)
		}
	}
}

func TestMCPFollowFeedCursorAllStoriesReadOnly(t *testing.T) {
	f := newMCPFixture(t)
	entity := mcpFollowTargets(t, f)
	client := f.connect(t, "2025-11-25")
	var target types.FollowTarget
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": entity.ID}, &target)
	date := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	var expected []string
	for i := 0; i < 7; i++ {
		article := mcpFollowArticle(t, f, "Pagination article", date, entity.ID, "")
		expected = append(expected, article.ID)
	}
	var page types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"target_ids": []string{entity.ID}, "limit": 2}, &page)
	var actual []string
	pages := 0
	for {
		pages++
		require.EqualValues(t, 7, page.Total)
		actual = append(actual, mcpFeedIDs(&page)...)
		if page.NextCursor == "" {
			break
		}
		require.Less(t, pages, 10)
		cursor := page.NextCursor
		page = types.FollowFeedResponse{}
		mcpCall(t, client, "follow_feed", map[string]any{"cursor": cursor, "limit": 2}, &page)
	}
	require.Equal(t, 4, pages)
	require.ElementsMatch(t, expected, actual)
}
