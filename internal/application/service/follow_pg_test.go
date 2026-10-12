package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

type followFixture struct {
	*entrySearchFixture
	follows *FollowService
}

func newFollowFixture(t *testing.T) *followFixture {
	t.Helper()
	h := newEntrySearchFixture(t)
	h.ctx = types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalWebUser, ID: "follow-owner"})
	entities := []types.Entity{entityRecord("entity-follow", "Followed company", "company"), entityRecord("entity-other", "Other company", "company")}
	require.NoError(t, h.db.Create(&entities).Error)
	require.NoError(t, h.db.Create(&types.Topic{ID: "topic-follow", SpaceID: 1, Name: "Followed research"}).Error)
	return &followFixture{entrySearchFixture: h, follows: NewFollowService(h.db)}
}

func (h *followFixture) link(t *testing.T, articleID string, entity, topic bool) {
	t.Helper()
	var when time.Time
	require.NoError(t, h.db.Table("entries").Select("created_at").Where("id=?", articleID).Scan(&when).Error)
	if entity {
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: articleID, EntityID: "entity-follow", CreatedAt: when}).Error)
	}
	if topic {
		require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: articleID, TopicID: "topic-follow", CreatedAt: when}).Error)
	}
}

func (h *followFixture) followBoth(t *testing.T) {
	t.Helper()
	_, err := h.follows.Follow(h.ctx, "entity", "entity-follow")
	require.NoError(t, err)
	_, err = h.follows.Follow(h.ctx, "topic", "topic-follow")
	require.NoError(t, err)
}

func (h *followFixture) collected(t *testing.T, id string, when time.Time) {
	t.Helper()
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", id).Updates(map[string]any{"created_at": when, "follow_ready_at": when}).Error)
	require.NoError(t, h.db.Table("entry_entities").Where("entry_id=?", id).Update("created_at", when).Error)
	require.NoError(t, h.db.Table("entry_topics").Where("entry_id=?", id).Update("created_at", when).Error)
}

func (h *followFixture) feed(t *testing.T, request types.FollowFeedRequest) *types.FollowFeedResponse {
	t.Helper()
	result, err := h.follows.Feed(h.ctx, request)
	require.NoError(t, err)
	return result
}

func followFeedIDs(result *types.FollowFeedResponse) []string {
	ids := make([]string, 0, len(result.Results))
	for _, item := range result.Results {
		ids = append(ids, item.EntryID)
	}
	return ids
}

func TestFollowsPostgresActionsIdempotencyAndLogs(t *testing.T) {
	h := newFollowFixture(t)
	for _, targetType := range []string{"entity", "topic"} {
		id := targetType + "-follow"
		first, err := h.follows.Follow(h.ctx, targetType, id)
		require.NoError(t, err)
		second, err := h.follows.Follow(h.ctx, targetType, id)
		require.NoError(t, err)
		require.Equal(t, first.CreatedAt.UnixMicro(), second.CreatedAt.UnixMicro())
		require.Equal(t, id, first.TargetID)
		require.NotEmpty(t, first.Name)
	}
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 2)
	var logs []types.OperationLog
	require.NoError(t, h.db.Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "follow", log.OperationType)
		require.Equal(t, "web_user:follow-owner", log.Actor)
		require.JSONEq(t, "null", string(log.Before))
		var after types.FollowTarget
		require.NoError(t, json.Unmarshal(log.After, &after))
		require.Equal(t, log.ObjectID, after.TargetID)
	}
	apiCtx := types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "follow-key"})
	for _, targetType := range []string{"entity", "topic"} {
		require.NoError(t, h.follows.Unfollow(apiCtx, targetType, targetType+"-follow"))
		require.NoError(t, h.follows.Unfollow(apiCtx, targetType, targetType+"-follow"))
	}
	items, err = h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Empty(t, items)
	logs = nil
	require.NoError(t, h.db.Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 4)
	for _, log := range logs[2:] {
		require.Equal(t, "unfollow", log.OperationType)
		require.Equal(t, "api_key:follow-key", log.Actor)
		require.JSONEq(t, "null", string(log.After))
	}
	otherCtx := context.WithValue(h.ctx, types.SpaceIDContextKey, uint64(2))
	_, err = h.follows.Follow(otherCtx, "entity", "entity-follow")
	require.Error(t, err)
	for _, pair := range [][2]string{{"article", "entity-follow"}, {"entity", ""}, {"topic", "missing"}} {
		_, err := h.follows.Follow(h.ctx, pair[0], pair[1])
		require.Error(t, err)
	}
	_, err = h.follows.Follow(context.Background(), "entity", "entity-follow")
	require.Error(t, err)
}

func TestFollowsPostgresLogFailureRollsBackChanges(t *testing.T) {
	h := newFollowFixture(t)
	_, err := h.follows.Follow(h.ctx, "entity", "entity-follow")
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_follow_log() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN RAISE EXCEPTION 'fixture log rejected'; END $$;
        CREATE TRIGGER reject_follow_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_follow_log()`).Error)
	_, err = h.follows.Follow(h.ctx, "topic", "topic-follow")
	require.Error(t, err)
	require.Error(t, h.follows.Unfollow(h.ctx, "entity", "entity-follow"))
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "entity-follow", items[0].TargetID)
}

func TestFollowsPostgresConcurrentActionsRemainIdempotent(t *testing.T) {
	h := newFollowFixture(t)
	run := func(action func() error) {
		var wg sync.WaitGroup
		failures := make(chan error, 8)
		start := make(chan struct{})
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				failures <- action()
			}()
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
	}
	for _, pair := range [][2]string{{"entity", "entity-follow"}, {"topic", "topic-follow"}} {
		run(func() error { _, err := h.follows.Follow(h.ctx, pair[0], pair[1]); return err })
		run(func() error { return h.follows.Unfollow(h.ctx, pair[0], pair[1]) })
	}
	var count int64
	require.NoError(t, h.db.Model(&types.OperationLog{}).Count(&count).Error)
	require.EqualValues(t, 4, count)
	require.NoError(t, h.db.Model(&types.Follow{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestFollowsPostgresMergeTransferDeduplicationAndDeletion(t *testing.T) {
	for _, followKeep := range []bool{false, true} {
		t.Run(fmt.Sprint(followKeep), func(t *testing.T) {
			h := newFollowFixture(t)
			_, err := h.follows.Follow(h.ctx, "entity", "entity-other")
			require.NoError(t, err)
			earliest := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
			require.NoError(t, h.db.Model(&types.Follow{}).Where("target_id='entity-other'").Update("created_at", earliest).Error)
			if followKeep {
				_, err = h.follows.Follow(h.ctx, "entity", "entity-follow")
				require.NoError(t, err)
			}
			article := h.add(t, "merged-article", "article", "2026-10-01", "event", "semanticlaunch", 0)
			require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: article.ID, EntityID: "entity-other"}).Error)
			require.NoError(t, h.db.Model(&types.KnowledgeBase{}).Where("id='search-kb'").Update("summary_model_id", "entity-chat").Error)
			entities := NewEntityService(h.db, entityTestModels{model: &entityCountingChat{}})
			require.NoError(t, entities.MergeEntities(h.ctx, "entity-follow", "entity-other"))
			items, err := h.follows.ListFollows(h.ctx)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, "entity-follow", items[0].TargetID)
			require.Equal(t, earliest.UnixMicro(), items[0].CreatedAt.UnixMicro())
			require.NoError(t, entities.MergeEntities(h.ctx, "entity-follow", "entity-other"))
			result := h.feed(t, types.FollowFeedRequest{})
			require.Equal(t, []string{article.ID}, followFeedIDs(result))
			require.Equal(t, "entity-follow", result.Results[0].MatchedFollows[0].TargetID)
			redirected, err := h.follows.Follow(h.ctx, "entity", "entity-other")
			require.NoError(t, err)
			require.Equal(t, "entity-follow", redirected.TargetID)
			require.NoError(t, h.follows.Unfollow(h.ctx, "entity", "entity-other"))
			items, err = h.follows.ListFollows(h.ctx)
			require.NoError(t, err)
			require.Empty(t, items)
		})
	}
	h := newFollowFixture(t)
	h.followBoth(t)
	topics := NewTopicService(h.db)
	topics.SetOperationLogs(NewOperationLogService(h.db))
	require.NoError(t, topics.DeleteTopic(h.ctx, "topic-follow"))
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "entity", items[0].TargetType)
	// Foreign keys also clean direct object deletion and forbid dangling targets.
	require.NoError(t, h.db.Where("id='entity-follow'").Delete(&types.Entity{}).Error)
	var count int64
	require.NoError(t, h.db.Model(&types.Follow{}).Count(&count).Error)
	require.Zero(t, count)
	require.Error(t, h.db.Create(&types.Follow{SpaceID: 1, TargetType: "entity", TargetID: "missing"}).Error)
	require.NoError(t, h.follows.Unfollow(h.ctx, "topic", "topic-follow"))
}

func TestFollowFeedPostgresEligibilityMatchesFiltersAndNewWatermark(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []string{"old", "entity-only", "topic-only", "both", "unrelated", "processing", "intake-review", "processing-review", "disabled", "deleted"} {
		h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0)
		h.collected(t, id, now.Add(-time.Hour))
		if id != "unrelated" {
			h.link(t, id, id != "topic-only", id == "topic-only" || id == "both")
		}
	}
	h.collected(t, "old", now.Add(-8*24*time.Hour))
	h.collected(t, "both", now.Add(-30*time.Minute))
	h.collected(t, "topic-only", now.Add(-45*time.Minute))
	for id, updates := range map[string]map[string]any{
		"processing":        {"parse_status": types.ParseStatusProcessing},
		"intake-review":     {"parse_status": types.ParseStatusRejected},
		"processing-review": {"processing_review_status": types.ProcessingReviewPending},
		"disabled":          {"enable_status": "disabled"},
		"deleted":           {"deleted_at": now},
	} {
		require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", id).Updates(updates).Error)
	}
	for _, source := range []string{"mine", "report"} {
		h.add(t, source, source, "2026-10-01", "", "semanticlaunch", 0)
		h.link(t, source, true, true)
	}
	request := types.FollowFeedRequest{IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()}
	result := h.feed(t, request)
	require.Equal(t, []string{"both", "topic-only", "entity-only", "old"}, followFeedIDs(result))
	require.EqualValues(t, 4, result.Total)
	require.EqualValues(t, 4, result.NewCount)
	require.Nil(t, result.LastViewedAt)
	for _, item := range result.Results {
		require.True(t, item.IsNew)
		require.NotEmpty(t, item.CollectedAt)
		require.Equal(t, "article", item.Source)
		require.NotEmpty(t, item.Summary)
		require.NotEmpty(t, item.Snippets)
	}
	require.Len(t, result.Results[0].MatchedFollows, 2)
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	for _, item := range items {
		require.EqualValues(t, 2, item.RecentArticleCount, "old, ineligible, mine and report do not contribute")
	}
	request.TargetIDs = []string{"topic-follow"}
	filtered := h.feed(t, request)
	require.Equal(t, []string{"both", "topic-only"}, followFeedIDs(filtered))
	require.Len(t, filtered.Results[0].MatchedFollows, 2, "filtering keeps every matched follow on an article")
	require.EqualValues(t, 4, filtered.NewCount, "sidebar new count is global")
	request.TargetIDs = nil
	checkpoint := now.Add(-time.Hour)
	state, err := h.follows.MarkViewed(h.ctx, &checkpoint)
	require.NoError(t, err)
	require.EqualValues(t, 2, state.NewCount)
	result = h.feed(t, request)
	require.True(t, result.Results[0].IsNew)
	require.True(t, result.Results[1].IsNew)
	require.False(t, result.Results[2].IsNew, "timestamp equality is already seen")
	require.False(t, result.Results[3].IsNew)
	defaultFeed := h.feed(t, types.FollowFeedRequest{CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, []string{"both", "topic-only"}, followFeedIDs(defaultFeed))
	explicit := h.feed(t, types.FollowFeedRequest{Since: now.Add(-9 * 24 * time.Hour).Format(time.RFC3339Nano), Limit: 1, CollapseSimilar: entrySearchNoCollapse()})
	require.EqualValues(t, 4, explicit.Total)
	require.Len(t, explicit.Results, 1)
	state, err = h.follows.State(h.ctx)
	require.NoError(t, err)
	require.Equal(t, checkpoint.UnixMicro(), state.LastViewedAt.UnixMicro(), "all feed reads preserve the checkpoint")
	// An article collected while the page is loading must survive acknowledging its snapshot.
	h.add(t, "arrived-after-open", "article", "2026-10-01", "event", "semanticlaunch", 0)
	h.link(t, "arrived-after-open", true, false)
	state, err = h.follows.MarkViewed(h.ctx, &defaultFeed.ViewedAt)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.NewCount)
	state, err = h.follows.MarkViewed(h.ctx, &checkpoint)
	require.NoError(t, err)
	require.Equal(t, defaultFeed.ViewedAt.UnixMicro(), state.LastViewedAt.UnixMicro(), "stale page cannot move the checkpoint backwards")
	state, err = h.follows.MarkViewed(h.ctx, nil)
	require.NoError(t, err)
	require.Zero(t, state.NewCount)
	require.Empty(t, h.feed(t, types.FollowFeedRequest{}).Results)
	require.Len(t, h.feed(t, request).Results, 5)
	future := time.Now().Add(time.Hour)
	_, err = h.follows.MarkViewed(h.ctx, &future)
	require.Error(t, err)
	for _, invalid := range []types.FollowFeedRequest{{Since: "invalid"}, {Limit: 101}, {Limit: -1}, {TargetIDs: []string{""}}} {
		_, err := h.follows.Feed(h.ctx, invalid)
		require.Error(t, err)
	}
	_, err = h.follows.Feed(h.ctx, types.FollowFeedRequest{Since: "2026-01-01"})
	require.NoError(t, err)
}

func TestFollowFeedPostgresSimilarReportsRulesAndChildMetadata(t *testing.T) {
	for _, scenario := range []string{"identical", "different-model", "different-dimension", "missing-publication", "outside-window", "all-pairs"} {
		t.Run(scenario, func(t *testing.T) {
			h := newFollowFixture(t)
			h.followBoth(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			for i, id := range []string{"a", "b"} {
				h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0)
				h.link(t, id, i == 0, i == 1)
				h.collected(t, id, now.Add(time.Duration(-i-1)*time.Hour))
			}
			expected := 2
			switch scenario {
			case "identical":
				expected = 1
			case "different-model":
				require.NoError(t, h.db.Table("article_embeddings").Where("entry_id='b'").Update("model_id", "other-model").Error)
			case "different-dimension":
				require.NoError(t, h.db.Exec("UPDATE article_embeddings SET dimension=2,embedding=? WHERE entry_id='b'", pgvector.NewHalfVector([]float32{1, 0})).Error)
			case "missing-publication":
				require.NoError(t, h.db.Model(&types.Entry{}).Where("id='b'").Update("metadata", types.JSON(`{"original_url":"https://example.org/b"}`)).Error)
			case "outside-window":
				require.NoError(t, h.db.Model(&types.Entry{}).Where("id='b'").Update("metadata", types.JSON(`{"published_at":"2020-01-01"}`)).Error)
			case "all-pairs":
				h.add(t, "c", "article", "2026-10-01", "event", "semanticlaunch", 0)
				h.link(t, "c", true, true)
				h.collected(t, "c", now.Add(-3*time.Hour))
				config := &types.RetrievalConfig{SimilarReportsThreshold: 0.95}
				require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("retrieval_config", config).Error)
				for i, id := range []string{"a", "b", "c"} {
					angle := float64(i) * math.Pi / 12
					require.NoError(t, h.db.Exec("UPDATE article_embeddings SET embedding=? WHERE entry_id=?", pgvector.NewHalfVector([]float32{float32(math.Cos(angle)), float32(math.Sin(angle)), 0}), id).Error)
				}
			}
			result := h.feed(t, types.FollowFeedRequest{IncludeSeen: true})
			require.Len(t, result.Results, expected)
			require.Equal(t, "a", result.Results[0].EntryID, "newest collection represents the group")
			if scenario == "identical" || scenario == "all-pairs" {
				require.Len(t, result.Results[0].SimilarReports, 1)
				child := result.Results[0].SimilarReports[0]
				require.Equal(t, "b", child.EntryID)
				require.True(t, child.IsNew)
				require.Equal(t, "topic-follow", child.MatchedFollows[0].TargetID)
				require.NotEmpty(t, child.CollectedAt)
				raw, err := json.Marshal(result)
				require.NoError(t, err)
				var serialized struct {
					Results []struct {
						SimilarReports []struct {
							IsNew bool `json:"is_new"`
						} `json:"similar_reports"`
					} `json:"results"`
				}
				require.NoError(t, json.Unmarshal(raw, &serialized))
				require.True(t, serialized.Results[0].SimilarReports[0].IsNew, "JSON preserves follow metadata in folded articles")
			}
			unfolded := h.feed(t, types.FollowFeedRequest{IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()})
			require.EqualValues(t, len(unfolded.Results), result.NewCount)
			require.Equal(t, unfolded.Total, result.Total)
		})
	}
}

func TestFollowsPostgresMergeLogFailureRollsBackTransfer(t *testing.T) {
	h := newFollowFixture(t)
	_, err := h.follows.Follow(h.ctx, "entity", "entity-other")
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_merge_follow_log() RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN IF NEW.operation_type='merge_entities' THEN RAISE EXCEPTION 'fixture merge log rejected'; END IF; RETURN NEW; END $$;
        CREATE TRIGGER reject_merge_follow_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_merge_follow_log()`).Error)
	require.Error(t, NewEntityService(h.db, nil).MergeEntities(h.ctx, "entity-follow", "entity-other"))
	items, err := h.follows.ListFollows(h.ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "entity-other", items[0].TargetID)
	var merged types.Entity
	require.NoError(t, h.db.Where("id='entity-other'").First(&merged).Error)
	require.Empty(t, merged.MergedIntoID)
}

func TestFollowFeedPostgresLateEligibilityAndAssociationTimes(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	old := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
	for _, id := range []string{"processing-late", "recognized-late", "association-late", "historical"} {
		h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0)
		h.collected(t, id, old)
	}
	h.link(t, "processing-late", true, false)
	require.NoError(t, h.db.Table("entries").Where("id='processing-late'").Update("parse_status", types.ParseStatusProcessing).Error)
	require.NoError(t, h.db.Table("entries").Where("id='recognized-late'").Update("parse_status", types.ParseStatusProcessing).Error)
	opened := h.feed(t, types.FollowFeedRequest{IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()})
	_, err := h.follows.MarkViewed(h.ctx, &opened.ViewedAt)
	require.NoError(t, err)
	require.NoError(t, h.db.Table("entries").Where("id='processing-late'").Update("parse_status", types.ParseStatusCompleted).Error)
	require.NoError(t, h.db.Model(&types.KnowledgeBase{}).Where("id='search-kb'").Update("summary_model_id", "entity-chat").Error)
	entities := NewEntityService(h.db, entityTestModels{model: &entityCountingChat{}})
	_, err = entities.ResolveEntities(h.ctx, "recognized-late", []types.EntityCandidate{{Name: "Followed company", EntityType: "company"}})
	require.NoError(t, err)
	require.NoError(t, h.db.Table("entries").Where("id='recognized-late'").Update("parse_status", types.ParseStatusCompleted).Error)
	// A new relation can arrive after qualification, including in a reprocessing write.
	_, err = entities.ResolveEntities(h.ctx, "association-late", []types.EntityCandidate{{Name: "Followed company", EntityType: "company"}})
	require.NoError(t, err)
	require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: "historical", TopicID: "topic-follow", Source: types.TopicSourceModel, IsHistorical: true}).Error)
	result := h.feed(t, types.FollowFeedRequest{CollapseSimilar: entrySearchNoCollapse()})
	require.ElementsMatch(t, []string{"processing-late", "recognized-late", "association-late"}, followFeedIDs(result))
	require.EqualValues(t, 3, result.NewCount)
	for _, item := range result.Results {
		require.True(t, item.IsNew)
		require.True(t, item.EnteredAt.After(opened.ViewedAt))
		require.True(t, item.CollectedAt.Before(opened.ViewedAt))
	}
	all := h.feed(t, types.FollowFeedRequest{IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()})
	require.Len(t, all.Results, 4)
	for _, item := range all.Results {
		if item.EntryID == "historical" {
			require.Nil(t, item.EnteredAt)
			require.False(t, item.IsNew)
		}
	}
	// Adding a follow never changes an old article's entrance time.
	require.NoError(t, h.db.Create(&types.Topic{SpaceID: 1, ID: "history-follow", Name: "New historical follow"}).Error)
	require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: "historical", TopicID: "history-follow", CreatedAt: old}).Error)
	_, err = h.follows.Follow(h.ctx, "topic", "history-follow")
	require.NoError(t, err)
	require.Empty(t, h.feed(t, types.FollowFeedRequest{TargetIDs: []string{"history-follow"}}).Results)
	state, err := h.follows.MarkViewed(h.ctx, nil)
	require.NoError(t, err)
	require.Zero(t, state.NewCount)
}

func TestFollowFeedPostgresCursorWindowAndFoldedPagination(t *testing.T) {
	for _, fold := range []bool{false, true} {
		t.Run(fmt.Sprint(fold), func(t *testing.T) {
			h := newFollowFixture(t)
			h.followBoth(t)
			date := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			for _, id := range []string{"a", "b", "c", "d", "e"} {
				h.add(t, id, "article", "2026-10-01", "event", "semanticlaunch", 0)
				h.link(t, id, true, false)
				h.collected(t, id, date)
			}
			// Keep one pair folded and the other articles outside its publication window.
			for _, id := range []string{"c", "d", "e"} {
				require.NoError(t, h.db.Table("entries").Where("id=?", id).Update("metadata", types.JSON(`{"published_at":"2020-01-01"}`)).Error)
			}
			checkpoint := date.Add(-time.Second)
			_, err := h.follows.MarkViewed(h.ctx, &checkpoint)
			require.NoError(t, err)
			request := types.FollowFeedRequest{Limit: 1, CollapseSimilar: &fold}
			page := h.feed(t, request)
			require.NotEmpty(t, page.NextCursor)
			cutoff := page.ViewedAt
			h.add(t, "arrival", "article", "2026-10-01", "event", "semanticlaunch", 0)
			h.link(t, "arrival", true, false)
			_, err = h.follows.MarkViewed(h.ctx, nil)
			require.NoError(t, err)
			var ids []string
			for {
				require.Equal(t, cutoff, page.ViewedAt)
				require.EqualValues(t, 5, page.Total)
				for _, item := range page.Results {
					ids = append(ids, item.EntryID)
					for _, child := range item.SimilarReports {
						ids = append(ids, child.EntryID)
					}
				}
				if page.NextCursor == "" {
					break
				}
				page = h.feed(t, types.FollowFeedRequest{Cursor: page.NextCursor, Limit: 1})
			}
			require.ElementsMatch(t, []string{"a", "b", "c", "d", "e"}, ids)
			_, err = h.follows.Feed(h.ctx, types.FollowFeedRequest{Cursor: "bad"})
			require.Error(t, err)
			request.Since = date.Add(-time.Second).Format(time.RFC3339Nano)
			first := h.feed(t, request)
			_, err = h.follows.Feed(h.ctx, types.FollowFeedRequest{Cursor: first.NextCursor, TargetIDs: []string{"topic-follow"}})
			require.Error(t, err)
		})
	}
}

func TestFollowFeedPostgresRealTopicBackfillPreservesHistoricalLinks(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	topics, _, ctx := newTopicManagementService(t, h)
	articles := historicalTopicArticles(t, h, 2)
	follows := NewFollowService(h.db)
	_, err := follows.MarkViewed(ctx, nil)
	require.NoError(t, err)
	topic, err := topics.CreateTopic(ctx, &types.Topic{Name: "Historical follow", Description: "Products"})
	require.NoError(t, err)
	_, err = follows.Follow(ctx, "topic", topic.ID)
	require.NoError(t, err)
	runTopicJob(t, topics, ctx, topic.ID)
	links := topicLinks(t, topics, topic.ID)
	require.Len(t, links, 2)
	for _, link := range links {
		require.True(t, link.IsHistorical)
	}
	feed, err := follows.Feed(ctx, types.FollowFeedRequest{IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()})
	require.NoError(t, err)
	require.Len(t, feed.Results, 2)
	require.Zero(t, feed.NewCount)
	for _, item := range feed.Results {
		require.False(t, item.IsNew)
		require.Nil(t, item.EnteredAt)
	}
	fresh, err := follows.Feed(ctx, types.FollowFeedRequest{})
	require.NoError(t, err)
	require.Empty(t, fresh.Results)
	// Replacing unchanged model topics preserves their association origin and timestamp.
	var original types.EntryTopic
	require.NoError(t, h.db.Where("entry_id=? AND topic_id=?", articles[0].ID, topic.ID).First(&original).Error)
	require.NoError(t, topics.ReplaceEntryTopics(ctx, articles[0].ID, []string{topic.ID}))
	var preserved types.EntryTopic
	require.NoError(t, h.db.Where("entry_id=? AND topic_id=?", articles[0].ID, topic.ID).First(&preserved).Error)
	require.True(t, preserved.IsHistorical)
	require.Equal(t, original.CreatedAt.UnixMicro(), preserved.CreatedAt.UnixMicro())
}
