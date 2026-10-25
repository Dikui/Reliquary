package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFollowStoryHTTPActionsAndRemovedReadState(t *testing.T) {
	f := newMCPFixture(t)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	story := types.Story{
		ID:          "follow-story",
		SpaceID:     1,
		Title:       "A developing story",
		FirstFactAt: stamp,
		LastFactAt:  stamp,
	}
	require.NoError(t, f.db.Create(&story).Error)
	for range 2 {
		r := followRequest(t, f, http.MethodPut, "/api/v1/follows/story/"+story.ID, nil)
		require.Equal(t, 200, r.Code, r.Body.String())
	}
	r := followRequest(t, f, http.MethodGet, "/api/v1/follows", nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	var targets struct{ Data []types.FollowTarget }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &targets))
	require.Len(t, targets.Data, 1)
	require.Equal(t, story.ID, targets.Data[0].TargetID)
	for range 2 {
		r = followRequest(t, f, http.MethodDelete, "/api/v1/follows/story/"+story.ID, nil)
		require.Equal(t, 200, r.Code, r.Body.String())
	}
	var logs []types.OperationLog
	require.NoError(t, f.db.Where("operation_type IN ('follow','unfollow')").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, path := range []string{"/api/v1/follows/state", "/api/v1/follows/view"} {
		method := http.MethodGet
		if path == "/api/v1/follows/view" {
			method = http.MethodPost
		}
		r = followRequest(t, f, method, path, nil)
		require.Equal(t, 404, r.Code, r.Body.String())
	}
}

func followRequest(t *testing.T, f *mcpFixture, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload := ""
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		payload = string(data)
	}
	return personalRequest(t, f.router, method, path, payload, f.key, true)
}

func TestFollowStoryHTTPFeedUsesFactEndpointsAndCreationTime(t *testing.T) {
	f := newMCPFixture(t)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	entity := mcpFollowTargets(t, f)
	r := followRequest(t, f, http.MethodPut, "/api/v1/follows/entity/"+entity.ID, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	story := types.Story{
		ID:          "endpoint-story",
		SpaceID:     1,
		Title:       "Endpoint progress",
		FirstFactAt: stamp,
		LastFactAt:  stamp,
	}
	require.NoError(t, f.db.Create(&story).Error)
	fact := types.Fact{
		ID:              "endpoint-fact",
		SpaceID:         1,
		StoryID:         story.ID,
		Title:           "A new fact",
		Subject:         "Acme",
		Action:          "released",
		SubjectEntityID: &entity.ID,
		CreatedAt:       stamp,
	}
	require.NoError(t, f.db.Create(&fact).Error)
	entry := f.seedArticle(t, "Original evidence", "full", "# Evidence\n")
	require.NoError(
		t,
		f.db.Create(
			&types.EntryFact{
				SpaceID:   1,
				EntryID:   entry.ID,
				FactID:    fact.ID,
				Role:      "source",
				Quote:     "Evidence",
				CreatedAt: stamp,
			},
		).Error,
	)
	r = followRequest(t, f, http.MethodPost, "/api/v1/follows/feed", map[string]any{})
	require.Equal(t, 200, r.Code, r.Body.String())
	var feed struct {
		Data struct {
			Results []struct {
				Story       types.Story
				TriggerFact types.StoryFact   `json:"trigger_fact"`
				RecentFacts []types.StoryFact `json:"recent_facts"`
			}
		}
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &feed))
	require.Len(t, feed.Data.Results, 1, "fact endpoints trigger follows without entry entity links")
	require.Equal(t, story.ID, feed.Data.Results[0].Story.ID)
	require.Equal(t, fact.ID, feed.Data.Results[0].TriggerFact.ID)
	require.Len(t, feed.Data.Results[0].RecentFacts, 1)
}

func followFeedHTTP(t *testing.T, f *mcpFixture, request types.FollowFeedRequest) types.FollowFeedResponse {
	t.Helper()
	r := followRequest(t, f, http.MethodPost, "/api/v1/follows/feed", request)
	require.Equal(t, 200, r.Code, r.Body.String())
	var out struct{ Data types.FollowFeedResponse }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	return out.Data
}

func seedFollowStory(t *testing.T, f *mcpFixture, id string, at time.Time) types.Story {
	t.Helper()
	story := types.Story{ID: id, SpaceID: 1, Title: id, FirstFactAt: at, LastFactAt: at}
	require.NoError(t, f.db.Create(&story).Error)
	return story
}

func seedFollowFact(
	t *testing.T,
	f *mcpFixture,
	storyID, id string,
	at time.Time,
	subjectID, objectID *string,
	libraryID string,
) types.Fact {
	t.Helper()
	fact := types.Fact{
		ID:              id,
		SpaceID:         1,
		StoryID:         storyID,
		Title:           id,
		Subject:         "Institute",
		Action:          "released",
		SubjectEntityID: subjectID,
		ObjectEntityID:  objectID,
		CreatedAt:       at,
	}
	require.NoError(t, f.db.Create(&fact).Error)
	if libraryID != "" {
		entry := types.Entry{
			ID:           id + "-source",
			SpaceID:      1,
			LibraryID:    libraryID,
			Type:         "manual",
			Title:        id + " evidence",
			ParseStatus:  types.ParseStatusCompleted,
			EnableStatus: "enabled",
		}
		require.NoError(t, f.db.Create(&entry).Error)
		require.NoError(
			t,
			f.db.Create(
				&types.EntryFact{
					SpaceID:   1,
					EntryID:   entry.ID,
					FactID:    fact.ID,
					Role:      "source",
					Quote:     "Original evidence",
					CreatedAt: at,
				},
			).Error,
		)
	}
	return fact
}

func TestFollowStoryHTTPFeedFiltersSortingSourcesAndVisibility(t *testing.T) {
	f := newMCPFixture(t)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	entity := mcpFollowTargets(t, f)
	r := followRequest(t, f, http.MethodPut, "/api/v1/follows/entity/"+entity.ID, nil)
	require.Equal(t, 200, r.Code, r.Body.String())
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	disabled := types.Library{
		ID:                 "feed-disabled",
		SpaceID:            1,
		Name:               "Disabled",
		Directory:          "feed-disabled",
		Template:           types.TemplateExternalTimely,
		SourceEvidenceRule: "link_required",
	}
	require.NoError(t, f.db.Create(&disabled).Error)
	for _, id := range []string{"old", "new", "disabled", "hidden", "manual", "entry-link-only", "direct"} {
		seedFollowStory(t, f, id, now)
	}
	old := seedFollowFact(t, f, "old", "old-trigger", now, &entity.ID, nil, f.library.ID)
	for i := range 4 {
		seedFollowFact(
			t,
			f,
			"old",
			fmt.Sprintf("old-recent-%d", i),
			now.Add(time.Duration(i+1)*time.Minute),
			nil,
			nil,
			f.library.ID,
		)
	}
	newest := seedFollowFact(t, f, "new", "new-trigger", now.Add(5*time.Minute), nil, &entity.ID, f.library.ID)
	seedFollowFact(t, f, "disabled", "disabled-trigger", now.Add(20*time.Minute), &entity.ID, nil, disabled.ID)
	seedFollowFact(t, f, "old", "disabled-recent", now.Add(20*time.Minute), nil, nil, disabled.ID)
	seedFollowFact(t, f, "hidden", "hidden-trigger", now.Add(25*time.Minute), &entity.ID, nil, "")
	manual := seedFollowFact(t, f, "manual", "manual-trigger", now.Add(-time.Minute), &entity.ID, nil, "")
	require.NoError(t, f.db.Model(&manual).Update("manual_corrected", true).Error)
	entryOnly := seedFollowFact(t, f, "entry-link-only", "entry-only", now.Add(15*time.Minute), nil, nil, f.library.ID)
	require.NoError(
		t,
		f.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entryOnly.ID + "-source", EntityID: entity.ID}).Error,
	)
	seedFollowFact(t, f, "direct", "direct-trigger", now.Add(-2*time.Minute), nil, nil, f.library.ID)
	for _, id := range []string{"direct", "hidden", "disabled"} {
		r = followRequest(t, f, http.MethodPut, "/api/v1/follows/story/"+id, nil)
		require.Equal(t, 200, r.Code, r.Body.String())
	}
	page := followFeedHTTP(t, f, types.FollowFeedRequest{})
	require.Equal(t, []string{"new", "old", "manual", "direct"}, mcpFeedIDs(&page))
	require.Equal(t, newest.ID, page.Results[0].TriggerFact.ID, "either endpoint qualifies")
	require.Equal(t, old.ID, page.Results[1].TriggerFact.ID, "trigger remains visible outside recent three")
	require.Equal(
		t,
		[]string{"old-recent-3", "old-recent-2", "old-recent-1"},
		[]string{
			page.Results[1].RecentFacts[0].ID,
			page.Results[1].RecentFacts[1].ID,
			page.Results[1].RecentFacts[2].ID,
		},
	)
	require.Equal(t, now.Add(4*time.Minute), page.Results[1].Story.LastFactAt, "disabled facts cannot reorder a card")
	require.True(t, page.Results[2].TriggerFact.NoSource)
	require.Empty(t, page.Results[2].TriggerFact.Sources)
	// A later independent source adds evidence without changing the fact arrival time.
	extra := f.seedArticle(t, "Later source", "full", "Original evidence")
	require.NoError(
		t,
		f.db.Create(
			&types.EntryFact{
				SpaceID:   1,
				EntryID:   extra.ID,
				FactID:    old.ID,
				Role:      "source",
				Quote:     "Original evidence",
				CreatedAt: now.Add(30 * time.Minute),
			},
		).Error,
	)
	disabledEntry := types.Entry{
		ID:        "disabled-extra",
		SpaceID:   1,
		LibraryID: disabled.ID,
		Title:     "Excluded source",
		Type:      "manual",
	}
	require.NoError(t, f.db.Create(&disabledEntry).Error)
	require.NoError(
		t,
		f.db.Create(
			&types.EntryFact{
				SpaceID:   1,
				EntryID:   disabledEntry.ID,
				FactID:    old.ID,
				Role:      "source",
				Quote:     "Excluded",
				CreatedAt: now,
			},
		).Error,
	)
	after := followFeedHTTP(t, f, types.FollowFeedRequest{})
	require.Equal(t, mcpFeedIDs(&page), mcpFeedIDs(&after))
	require.Equal(t, page.Results[1].Story.LastFactAt, after.Results[1].Story.LastFactAt)
	require.Len(t, after.Results[1].TriggerFact.Sources, 2)
	for _, source := range after.Results[1].TriggerFact.Sources {
		require.Equal(t, f.library.ID, source.LibraryID)
	}
	filtered := followFeedHTTP(t, f, types.FollowFeedRequest{TargetIDs: []string{"direct"}})
	require.Equal(t, []string{"direct"}, mcpFeedIDs(&filtered))
	require.Empty(t, followFeedHTTP(t, f, types.FollowFeedRequest{Libraries: []string{disabled.Directory}}).Results)
	since := followFeedHTTP(t, f, types.FollowFeedRequest{Since: now.Add(4 * time.Minute).Format(time.RFC3339Nano)})
	require.Equal(t, []string{"new"}, mcpFeedIDs(&since))
	// HTTP and MCP expose the same card shape and no viewing metadata.
	var mcpFeed types.FollowFeedResponse
	mcpCall(t, f.connect(t, "2025-11-25"), "follow_feed", map[string]any{}, &mcpFeed)
	require.Equal(t, after, mcpFeed)
	r = followRequest(t, f, http.MethodPost, "/api/v1/follows/feed", map[string]any{})
	for _, removed := range []string{"last_viewed_at", "viewed_at", "new_count", "is_new", "entry_id\":\"entry-only"} {
		require.NotContains(t, r.Body.String(), removed)
	}
	// Withdrawing every source hides an ordinary fact immediately, without moving IDs.
	require.NoError(t, f.db.Where("fact_id=?", newest.ID).Delete(&types.EntryFact{}).Error)
	remaining := followFeedHTTP(t, f, types.FollowFeedRequest{})
	require.Equal(t, []string{"old", "manual", "direct"}, mcpFeedIDs(&remaining))
}

func TestFollowStoryHTTPCursorKeepsFactWindowAndValidatesScope(t *testing.T) {
	f := newMCPFixture(t)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	entity := mcpFollowTargets(t, f)
	require.Equal(t, 200, followRequest(t, f, http.MethodPut, "/api/v1/follows/entity/"+entity.ID, nil).Code)
	stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	for _, id := range []string{"a", "b", "c"} {
		seedFollowStory(t, f, id, stamp)
		seedFollowFact(t, f, id, id+"-fact", stamp, &entity.ID, nil, f.library.ID)
	}
	page := followFeedHTTP(t, f, types.FollowFeedRequest{Limit: 1})
	require.Equal(t, []string{"a"}, mcpFeedIDs(&page))
	require.NotEmpty(t, page.NextCursor)
	seedFollowStory(t, f, "late", time.Now().UTC())
	seedFollowFact(t, f, "late", "late-fact", time.Now().UTC(), &entity.ID, nil, f.library.ID)
	next := followFeedHTTP(t, f, types.FollowFeedRequest{Cursor: page.NextCursor, Limit: 1})
	require.Equal(t, []string{"b"}, mcpFeedIDs(&next))
	require.EqualValues(t, 3, next.Total)
	last := followFeedHTTP(t, f, types.FollowFeedRequest{Cursor: next.NextCursor, Limit: 1})
	require.Equal(t, []string{"c"}, mcpFeedIDs(&last))
	require.Empty(t, last.NextCursor)
	for _, request := range []types.FollowFeedRequest{
		{Cursor: "bad"},
		{Cursor: page.NextCursor, TargetIDs: []string{"other"}},
		{Libraries: []string{"unknown"}},
		{Limit: 101},
		{Since: "invalid"},
		{TargetIDs: []string{""}},
	} {
		r := followRequest(t, f, http.MethodPost, "/api/v1/follows/feed", request)
		require.Equal(t, 400, r.Code, r.Body.String())
	}
}

func TestFollowStoryHTTPManualMergeChainTransfersAndDeduplicates(t *testing.T) {
	for _, followKeep := range []bool{false, true} {
		t.Run(fmt.Sprint(followKeep), func(t *testing.T) {
			f := newMCPFixture(t)
			RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
			stamp := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
			for _, id := range []string{"merge-a", "merge-b", "merge-c"} {
				seedFollowStory(t, f, id, stamp)
				seedFollowFact(t, f, id, id+"-fact", stamp, nil, nil, f.library.ID)
			}
			require.Equal(t, 200, followRequest(t, f, http.MethodPut, "/api/v1/follows/story/merge-a", nil).Code)
			require.NoError(
				t,
				f.db.Model(&types.Follow{}).Where("target_id='merge-a'").Update("created_at", stamp).Error,
			)
			if followKeep {
				for _, id := range []string{"merge-b", "merge-c"} {
					require.Equal(t, 200, followRequest(t, f, http.MethodPut, "/api/v1/follows/story/"+id, nil).Code)
				}
			}
			for _, pair := range [][2]string{{"merge-a", "merge-b"}, {"merge-b", "merge-c"}, {"merge-a", "merge-c"}} {
				r := followRequest(
					t,
					f,
					http.MethodPost,
					"/api/v1/stories/"+pair[0]+"/merge",
					map[string]string{"keep_story_id": pair[1]},
				)
				require.Equal(t, 200, r.Code, r.Body.String())
			}
			r := followRequest(t, f, http.MethodGet, "/api/v1/follows", nil)
			var targets struct{ Data []types.FollowTarget }
			require.NoError(t, json.Unmarshal(r.Body.Bytes(), &targets))
			require.Len(t, targets.Data, 1)
			require.Equal(t, "merge-c", targets.Data[0].TargetID)
			require.Equal(t, stamp, targets.Data[0].CreatedAt)
			feed := followFeedHTTP(t, f, types.FollowFeedRequest{})
			require.Equal(t, []string{"merge-c"}, mcpFeedIDs(&feed))
			// Both HTTP and MCP accept an old ID after multiple merges.
			var redirected types.FollowTarget
			mcpCall(
				t,
				f.connect(t, "2025-11-25"),
				"follow",
				map[string]any{"target_type": "story", "target_id": "merge-a"},
				&redirected,
			)
			require.Equal(t, "merge-c", redirected.TargetID)
			r = followRequest(t, f, http.MethodDelete, "/api/v1/follows/story/merge-a", nil)
			require.Equal(t, 200, r.Code, r.Body.String())
			require.Empty(t, followFeedHTTP(t, f, types.FollowFeedRequest{}).Results)
		})
	}
}

func TestFollowStoryHTTPMergeLogFailureRollsBackFollowTransfer(t *testing.T) {
	f := newMCPFixture(t)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	stamp := time.Now().UTC().Add(-time.Hour)
	for _, id := range []string{"rollback-a", "rollback-b"} {
		seedFollowStory(t, f, id, stamp)
		seedFollowFact(t, f, id, id+"-fact", stamp, nil, nil, f.library.ID)
	}
	require.Equal(t, 200, followRequest(t, f, http.MethodPut, "/api/v1/follows/story/rollback-a", nil).Code)
	require.NoError(t, f.db.Exec(`CREATE FUNCTION reject_story_merge_log() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.operation_type='merge_stories' THEN RAISE EXCEPTION 'fixture rejected merge'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_story_merge_log BEFORE INSERT ON operation_logs FOR EACH ROW
 EXECUTE FUNCTION reject_story_merge_log()`).Error)
	r := followRequest(
		t,
		f,
		http.MethodPost,
		"/api/v1/stories/rollback-a/merge",
		map[string]string{"keep_story_id": "rollback-b"},
	)
	require.Equal(t, 500, r.Code, r.Body.String())
	page := followFeedHTTP(t, f, types.FollowFeedRequest{})
	require.Equal(t, []string{"rollback-a"}, mcpFeedIDs(&page))
	require.Equal(t, "rollback-a", groupingDetail(t, f, "rollback-a").Story.ID)
	r = personalRequest(t, f.router, http.MethodPut, "/api/v1/follows/story/rollback-b", "", "invalid-key", true)
	require.Equal(t, http.StatusUnauthorized, r.Code, r.Body.String())
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/follows/feed", "{}", "invalid-key", true)
	require.Equal(t, http.StatusUnauthorized, r.Code, r.Body.String())
}
