package router

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
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
	article := f.seedArticle(t, title, types.ProcessingModeFull, "# "+title+"\n\nOriginal follow evidence.\n")
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

func TestMCPFollowsListFeedAndSeenState(t *testing.T) {
	f := newMCPFixture(t)
	entity, topic := mcpFollowTargets(t, f)
	client := f.connect(t, "2025-11-25")
	var followed types.FollowTarget
	for _, target := range []map[string]any{
		{"target_type": "entity", "target_id": entity.ID},
		{"target_type": "entity", "target_id": entity.ID},
		{"target_type": "topic", "target_id": topic.ID},
	} {
		mcpCall(t, client, "follow", target, &followed)
		require.Equal(t, target["target_type"], followed.TargetType)
		require.Equal(t, target["target_id"], followed.TargetID)
		require.NotEmpty(t, followed.Name)
		require.False(t, followed.CreatedAt.IsZero())
	}

	now := time.Now().UTC().Truncate(time.Second)
	old := mcpFollowArticle(t, f, "Older than seven days", now.Add(-8*24*time.Hour), entity.ID, "")
	seen := mcpFollowArticle(t, f, "Article at viewed checkpoint", now.Add(-2*time.Hour), entity.ID, "")
	shared := mcpFollowArticle(t, f, "Shared follow article", now.Add(-time.Hour), entity.ID, topic.ID)
	newest := mcpFollowArticle(t, f, "Newest topic article", now.Add(-30*time.Minute), "", topic.ID)
	mcpFollowArticle(t, f, "Unrelated article", now.Add(-10*time.Minute), "", "")
	for _, excluded := range []struct {
		title, source, parseStatus, reviewStatus string
	}{
		{"Pending article", "article", types.ParseStatusPending, ""},
		{"Rejected article", "article", types.ParseStatusRejected, ""},
		{"Processing review article", "article", types.ParseStatusCompleted, "pending"},
		{"User document", "mine", types.ParseStatusCompleted, ""},
		{"Generated report", "report", types.ParseStatusCompleted, ""},
	} {
		article := mcpFollowArticle(t, f, excluded.title, now.Add(-20*time.Minute), entity.ID, topic.ID)
		require.NoError(t, f.db.Model(article).Updates(map[string]any{"source_kind": excluded.source, "parse_status": excluded.parseStatus, "processing_review_status": excluded.reviewStatus}).Error)
	}
	for _, decision := range []string{"pending", "rejected", "error"} {
		article := mcpFollowArticle(t, f, "Unapproved intake "+decision, now.Add(-20*time.Minute), entity.ID, topic.ID)
		intake, err := article.IntakeMetadata()
		require.NoError(t, err)
		intake.Decision = decision
		require.NoError(t, article.SetIntakeMetadata(intake))
		require.NoError(t, f.db.Model(article).Update("metadata", article.Metadata).Error)
	}

	var listed struct {
		Follows []types.FollowTarget `json:"follows"`
	}
	mcpCall(t, client, "list_follows", map[string]any{}, &listed)
	require.Len(t, listed.Follows, 2)
	for _, target := range listed.Follows {
		require.EqualValues(t, 2, target.RecentArticleCount)
		if target.TargetType == "entity" {
			require.Equal(t, entity.CanonicalName, target.Name)
		} else {
			require.Equal(t, topic.Name, target.Name)
		}
	}

	follows := service.NewFollowService(f.db)
	var initial types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &initial)
	require.Equal(t, []string{newest.ID, shared.ID, seen.ID, old.ID}, mcpFeedIDs(&initial))
	require.Nil(t, initial.LastViewedAt)
	require.EqualValues(t, 4, initial.NewCount)
	for _, item := range initial.Results {
		require.True(t, item.IsNew)
	}
	state, err := follows.State(f.ctx)
	require.NoError(t, err)
	require.Nil(t, state.LastViewedAt)

	lastViewed := now.Add(-2 * time.Hour)
	marked, err := follows.MarkViewed(f.ctx, &lastViewed)
	require.NoError(t, err)
	require.NotNil(t, marked.LastViewedAt)
	require.WithinDuration(t, lastViewed, *marked.LastViewedAt, time.Microsecond)
	var feed types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &feed)
	require.Equal(t, []string{newest.ID, shared.ID}, mcpFeedIDs(&feed))
	require.EqualValues(t, 2, feed.NewCount)
	require.ElementsMatch(t, []string{entity.ID, topic.ID}, mcpMatchedIDs(feed.Results[1]))
	require.Equal(t, "article", feed.Results[1].Source)
	require.Equal(t, shared.Title, feed.Results[1].Title)
	require.Equal(t, shared.Source, feed.Results[1].OriginalURL)
	require.Equal(t, "MCP source summary", feed.Results[1].Summary.Overview)
	require.Equal(t, entity.ID, feed.Results[1].Entities[0].ID)
	require.Equal(t, topic.ID, feed.Results[1].Topics[0].ID)
	require.WithinDuration(t, now.Add(-time.Hour), feed.Results[1].CollectedAt, time.Microsecond)
	for _, item := range feed.Results {
		require.True(t, item.IsNew)
	}

	var filtered types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"target_ids": []string{entity.ID}, "collapse_similar": false}, &filtered)
	require.Equal(t, []string{shared.ID}, mcpFeedIDs(&filtered))
	require.ElementsMatch(t, []string{entity.ID, topic.ID}, mcpMatchedIDs(filtered.Results[0]))
	require.EqualValues(t, 2, filtered.NewCount)
	mcpCall(t, client, "follow_feed", map[string]any{"target_ids": []string{entity.ID, topic.ID}, "limit": 1, "collapse_similar": false}, &filtered)
	require.Equal(t, []string{newest.ID}, mcpFeedIDs(&filtered))
	require.EqualValues(t, 2, filtered.NewCount)
	mcpCall(t, client, "follow_feed", map[string]any{"since": now.Add(-time.Hour).Format(time.RFC3339), "collapse_similar": false}, &filtered)
	require.Equal(t, []string{newest.ID}, mcpFeedIDs(&filtered))
	var historical types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"since": now.Add(-10 * 24 * time.Hour).Format(time.RFC3339), "collapse_similar": false}, &historical)
	require.Equal(t, []string{newest.ID, shared.ID, seen.ID, old.ID}, mcpFeedIDs(&historical))
	require.True(t, historical.Results[0].IsNew)
	require.True(t, historical.Results[1].IsNew)
	require.False(t, historical.Results[2].IsNew)
	require.False(t, historical.Results[3].IsNew)
	mcpCall(t, client, "list_follows", map[string]any{}, &listed)
	unchanged, err := follows.State(f.ctx)
	require.NoError(t, err)
	require.WithinDuration(t, *marked.LastViewedAt, *unchanged.LastViewedAt, time.Microsecond)
	require.EqualValues(t, 2, unchanged.NewCount)

	for range 2 {
		var removed struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Following  bool   `json:"following"`
		}
		mcpCall(t, client, "unfollow", map[string]any{"target_type": "topic", "target_id": topic.ID}, &removed)
		require.Equal(t, topic.ID, removed.TargetID)
		require.Equal(t, "topic", removed.TargetType)
		require.False(t, removed.Following)
	}
	mcpCall(t, client, "list_follows", map[string]any{}, &listed)
	require.Len(t, listed.Follows, 1)
	require.Equal(t, entity.ID, listed.Follows[0].TargetID)
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": false}, &feed)
	require.Equal(t, []string{shared.ID}, mcpFeedIDs(&feed))
	require.Equal(t, []string{entity.ID}, mcpMatchedIDs(feed.Results[0]))
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("operation_type IN ?", []string{"follow", "unfollow"}).Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 3)
	require.Equal(t, "follow", logs[0].OperationType)
	require.Equal(t, "follow", logs[1].OperationType)
	require.Equal(t, "unfollow", logs[2].OperationType)
	for _, entry := range logs {
		require.Contains(t, entry.Actor, "api_key:")
	}
	_, err = follows.MarkViewed(f.ctx, nil)
	require.NoError(t, err)
	mcpCall(t, client, "follow_feed", map[string]any{}, &feed)
	require.Empty(t, feed.Results)
	require.Zero(t, feed.NewCount)
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
		require.NoError(t, f.db.Exec(`INSERT INTO article_embeddings(entry_id,space_id,knowledge_base_id,model_id,dimension,embedding,content_hash)
			VALUES (?,?,?,'mcp-embedding',3,?,'mcp-follow-fixture')`, article.ID, types.SingleSpaceID, f.kb.ID, pgvector.NewHalfVector([]float32{1, 0, 0})).Error)
	}
	var feed types.FollowFeedResponse
	mcpCall(t, client, "follow_feed", map[string]any{"collapse_similar": true}, &feed)
	require.Equal(t, []string{newer.ID}, mcpFeedIDs(&feed))
	require.Equal(t, []string{topic.ID}, mcpMatchedIDs(feed.Results[0]))
	require.False(t, feed.Results[0].Incomplete)
	require.Len(t, feed.Results[0].SimilarReports, 1)
	member := feed.Results[0].SimilarReports[0]
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
