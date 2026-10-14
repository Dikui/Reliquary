package router

import (
	"context"
	"testing"
	"time"

	"reliquary/internal/application/service"
	"reliquary/internal/types"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func mcpFollowTargets(t *testing.T, f *mcpFixture) (types.Entity, types.Topic) {
	t.Helper()
	entity := types.Entity{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, CanonicalName: "Follow Acme", NormalizedName: "follow acme", EntityType: "company"}
	topic := types.Topic{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Name: "Follow topic", Description: "MCP follow observations"}
	require.NoError(t, f.db.Create(&entity).Error)
	require.NoError(t, f.db.Create(&topic).Error)
	return entity, topic
}

func mcpFollowArticle(t *testing.T, f *mcpFixture, title string, collected time.Time, entityID, topicID string) *types.Entry {
	t.Helper()
	article := f.seedArticle(t, title, "full", "# "+title+"\n\nOriginal follow evidence.\n")
	require.NoError(t, f.db.Model(article).Updates(map[string]any{"created_at": collected, "follow_ready_at": collected}).Error)
	if entityID != "" {
		require.NoError(t, f.db.Create(&types.EntryEntity{SpaceID: types.SingleSpaceID, EntryID: article.ID, EntityID: entityID, CreatedAt: collected}).Error)
	}
	if topicID != "" {
		require.NoError(t, f.db.Create(&types.EntryTopic{SpaceID: types.SingleSpaceID, EntryID: article.ID, TopicID: topicID, Source: "user", CreatedAt: collected}).Error)
	}
	return article
}

func mcpFeedIDs(result *types.FollowFeedResponse) []string {
	ids := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		ids = append(ids, item.EntryID)
	}
	return ids
}

func mcpMatchedIDs(item *types.FollowFeedArticle) []string {
	ids := make([]string, 0, len(item.MatchedFollows))
	for _, target := range item.MatchedFollows {
		ids = append(ids, target.TargetID)
	}
	return ids
}

func TestMCPFollowFeedSimilarReportsKeepTheirMatches(t *testing.T) {
	f := newMCPFixture(t)
	entity, topic := mcpFollowTargets(t, f)
	client := f.connect(t, "2025-11-25")
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": entity.ID}, nil)
	mcpCall(t, client, "follow", map[string]any{"target_type": "topic", "target_id": topic.ID}, nil)
	now := time.Now().UTC().Truncate(time.Second)
	older := mcpFollowArticle(t, f, "Similar entity coverage", now.Add(-2*time.Hour), entity.ID, "")
	newer := mcpFollowArticle(t, f, "Similar topic coverage", now.Add(-time.Hour), "", topic.ID)
	require.NoError(t, f.db.Model(older).Update("incomplete", true).Error)
	for _, article := range []*types.Entry{older, newer} {
		require.NoError(t, f.db.Exec(`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash)
			VALUES (?,?,?,'mcp-embedding',3,?,'mcp-follow-fixture')`, article.ID, types.SingleSpaceID, f.library.ID, pgvector.NewHalfVector([]float32{1, 0, 0})).Error)
	}
	var feed types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": true}, &feed)
	require.Equal(t, f.library.Name, feed.Results[0].LibraryName)
	require.Equal(t, []string{newer.ID}, mcpFeedIDs(&feed))
	require.Equal(t, []string{topic.ID}, mcpMatchedIDs(feed.Results[0]))
	require.False(t, feed.Results[0].Incomplete)
	require.Len(t, feed.Results[0].SimilarReports, 1)
	member := feed.Results[0].SimilarReports[0]
	require.Equal(t, f.library.Name, member.LibraryName)
	require.Equal(t, older.ID, member.EntryID)
	require.True(t, member.Incomplete)
	require.Equal(t, []string{entity.ID}, mcpMatchedIDs(member))
	require.True(t, member.IsNew)
	require.WithinDuration(t, now.Add(-2*time.Hour), member.CollectedAt, time.Microsecond)
	require.EqualValues(t, 2, feed.NewCount)
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &feed)
	require.Equal(t, []string{newer.ID, older.ID}, mcpFeedIDs(&feed))
	require.Empty(t, feed.Results[0].SimilarReports)
	require.Empty(t, feed.Results[1].SimilarReports)
	require.False(t, feed.Results[0].Incomplete)
	require.True(t, feed.Results[1].Incomplete)
	for _, swapped := range []bool{false, true} {
		require.NoError(t, f.db.Model(newer).Update("incomplete", swapped).Error)
		require.NoError(t, f.db.Model(older).Update("incomplete", !swapped).Error)
		for _, collapse := range []bool{false, true} {
			var wire struct {
				Results []*mcpIncompleteArticle `json:"results"`
			}
			mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": collapse}, &wire)
			if collapse {
				require.Len(t, wire.Results, 1)
				require.Len(t, wire.Results[0].SimilarReports, 1)
			} else {
				require.Len(t, wire.Results, 2)
			}
			require.Equal(t, map[string]bool{newer.ID: swapped, older.ID: !swapped}, mcpIncompleteValues(t, wire.Results))
		}
	}
	state, err := service.NewFollowService(f.db).State(f.ctx)
	require.NoError(t, err)
	require.Nil(t, state.LastViewedAt)
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
	state, err := service.NewFollowService(f.db).State(f.ctx)
	require.NoError(t, err)
	require.Nil(t, state.LastViewedAt)
}

func TestMCPFollowMergedEntityIDResolvesToRetainedTarget(t *testing.T) {
	f := newMCPFixture(t)
	merged, _ := mcpFollowTargets(t, f)
	retained := types.Entity{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, CanonicalName: "Retained Acme", NormalizedName: "retained acme", EntityType: "company"}
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
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &feed)
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

func TestMCPFollowFeedCursorAllArticlesAndUnchangedCheckpoint(t *testing.T) {
	f := newMCPFixture(t)
	entity, _ := mcpFollowTargets(t, f)
	client := f.connect(t, "2025-11-25")
	var target types.FollowTarget
	mcpCall(t, client, "follow", map[string]any{"target_type": "entity", "target_id": entity.ID}, &target)
	date := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	var expected []string
	for i := 0; i < 7; i++ {
		article := mcpFollowArticle(t, f, "Pagination article", date, entity.ID, "")
		expected = append(expected, article.ID)
	}
	follows := service.NewFollowService(f.db)
	checkpoint := date.Add(-time.Second)
	_, err := follows.MarkViewed(f.ctx, &checkpoint)
	require.NoError(t, err)
	var page types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"target_ids": []string{entity.ID}, "limit": 2, "collapse_similar": false}, &page)
	cutoff := page.ViewedAt
	var actual []string
	pages := 0
	for {
		pages++
		require.Equal(t, cutoff, page.ViewedAt)
		require.EqualValues(t, 7, page.Total)
		actual = append(actual, mcpFeedIDs(&page)...)
		state, err := follows.State(f.ctx)
		require.NoError(t, err)
		require.Equal(t, checkpoint.UnixMicro(), state.LastViewedAt.UnixMicro())
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
