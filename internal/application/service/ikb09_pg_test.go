package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestIKB09TopicLibrariesAssignmentHistoryAndRetention(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, model, ctx := newTopicManagementService(t, h)
	historicalTopicArticles(t, h, 2)
	other := types.Library{ID: "other-topic-library", SpaceID: h.library.SpaceID, Name: "Other", Template: types.TemplateOwn}
	require.NoError(t, h.db.Create(&other).Error)
	outside := types.Entry{ID: "outside-topic", SpaceID: h.library.SpaceID, LibraryID: other.ID, Type: "manual", SourceKind: "mine", Description: "Historical summary", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, h.db.Create(&outside).Error)
	_, err := svc.ListTopics(ctx, "")
	require.Error(t, err)
	_, err = svc.CreateTopic(ctx, &types.Topic{Name: "Missing library"})
	require.Error(t, err)
	otherTopic, err := svc.CreateTopic(ctx, &types.Topic{LibraryID: other.ID, Name: "Scoped topic"})
	require.NoError(t, err)
	require.NotEmpty(t, otherTopic.ID)

	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Scoped topic", Description: "Products", LibraryID: h.library.ID})
	require.NoError(t, err)
	job := runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, 2, job.Total)
	require.Len(t, model.calls(), 1)
	require.Len(t, model.calls()[0].Articles, 2)
	require.NoError(t, svc.ReplaceEntryTopics(ctx, outside.ID, []string{topic.ID}))
	require.Len(t, topicLinks(t, svc, topic.ID), 2)
	require.Error(t, svc.SetUserEntryTopics(ctx, outside.ID, []string{topic.ID}))
	eligible, err := svc.TopicsForLibrary(ctx, other.ID)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	require.Equal(t, otherTopic.ID, eligible[0].ID)
	description := "Updated products"
	_, err = svc.UpdateTopic(ctx, topic.ID, nil, &description)
	require.NoError(t, err)
	job = runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, 2, job.Total)
	require.Len(t, topicLinks(t, svc, topic.ID), 2)
	eligible, err = svc.TopicsForLibrary(ctx, h.library.ID)
	require.NoError(t, err)
	require.Len(t, eligible, 1)
	_, err = svc.CreateTopic(ctx, &types.Topic{Name: "Invalid", LibraryID: "unknown"})
	require.Error(t, err)
}

func TestIKB09FeedLibrariesWatermarksFoldingAndEntityGroups(t *testing.T) {
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
		if item.library == h.library.ID {
			h.link(t, item.id, true, false)
		} else {
			entityID := "entity-" + item.library
			entity := types.Entity{ID: entityID, SpaceID: 1, LibraryID: item.library, CanonicalName: "Followed company", NormalizedName: "followed company", EntityType: "company"}
			require.NoError(t, h.db.Where("id=?", entityID).FirstOrCreate(&entity).Error)
			_, err := h.follows.Follow(h.ctx, "entity", entityID)
			require.NoError(t, err)
			require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: item.id, EntityID: entityID, CreatedAt: now.Add(-time.Minute)}).Error)
		}
	}
	state, err := h.follows.State(h.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, state.NewCount)
	require.Len(t, state.Libraries, 2)
	folded := h.feed(t, types.FollowFeedRequest{IncludeSeen: true})
	require.Len(t, folded.Results, 2, "similar items fold only within each library")
	only := h.feed(t, types.FollowFeedRequest{Libraries: []string{other.Directory}, IncludeSeen: true, CollapseSimilar: entrySearchNoCollapse()})
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
	require.Len(t, entity.LibraryGroups, 1, "entity profiles only contain their own library")
	counts := map[string]int{}
	for _, group := range entity.LibraryGroups {
		counts[group.LibraryID] = group.Count
		require.NotEmpty(t, group.LibraryName)
	}
	require.Equal(t, map[string]int{h.library.ID: 1}, counts)
}

func TestIKB09TopicUpdateRetainsLibrary(t *testing.T) {
	h := newFollowFixture(t)
	h.followBoth(t)
	h.add(t, "topic-scope", "article", "2026-10-01", "event", "semanticlaunch", 0)
	h.link(t, "topic-scope", false, true)
	name := "Renamed local topic"
	topic, err := NewTopicService(h.db).UpdateTopic(h.ctx, "topic-follow", &name, nil)
	require.NoError(t, err)
	require.Equal(t, h.library.ID, topic.LibraryID)
	require.Len(t, h.feed(t, types.FollowFeedRequest{IncludeSeen: true}).Results, 1)
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

func TestIKB09ProcessingOnlyReceivesApplicableTopics(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	topics := NewTopicService(h.db)
	allowed, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Allowed", LibraryID: h.library.ID})
	require.NoError(t, err)
	other := types.Library{ID: "other-processing-scope", SpaceID: h.library.SpaceID, Name: "Other", Template: types.TemplateOwn}
	require.NoError(t, h.db.Create(&other).Error)
	excluded, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Excluded", LibraryID: other.ID})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "event", nil, []string{allowed.ID, excluded.ID})
	entry := h.upload(t, "scope-model-input", "# Original product\n\nA complete product announcement with evidence.")
	require.Contains(t, h.processor.requests[0][1].Content, allowed.ID)
	require.NotContains(t, h.processor.requests[0][1].Content, excluded.ID)
	var links []types.EntryTopic
	require.NoError(t, h.db.Where("entry_id=?", entry.ID).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, allowed.ID, links[0].TopicID)
}
