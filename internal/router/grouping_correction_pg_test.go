package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualGroupingMoveHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	f.request(t, http.MethodPost, "/api/v1/facts/"+a.ID+"/move", map[string]string{"story_id": b.StoryID}, 200)
	require.Empty(t, groupingDetail(t, f.mcpFixture, a.StoryID).Facts)
	detail := groupingDetail(t, f.mcpFixture, b.StoryID)
	require.Len(t, detail.Facts, 2)
	var wire map[string]any
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/facts/"+a.ID, nil, 200), &wire))
	require.Equal(t, true, wire["data"].(map[string]any)["manual_corrected"])
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		t.Fatal("corrected material must not be regrouped")
		return ""
	}
	reprocessCorrected(t, f, a.Sources[0].EntryID)
	require.Equal(t, detail, groupingDetail(t, f.mcpFixture, b.StoryID))
	assertCorrectionLog(t, f, "move_fact", a.ID)
}

func TestManualGroupingMergeFactsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	mention := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "ai-output", "source_ref": "analysis", "content": "An analysis.",
		"mentioned_fact_ids": []string{a.ID, b.ID},
	})[0]["entry_id"].(string)
	for _, id := range []string{a.Sources[0].EntryID, b.Sources[0].EntryID, mention} {
		reprocessCorrected(t, f, id)
	}
	after := groupingDetail(t, f.mcpFixture, a.StoryID)
	require.Len(t, after.Facts, 1)
	require.Len(t, after.Facts[0].Sources, 2)
	require.Len(t, after.Mentions, 1)
	assertCorrectionLog(t, f, "merge_facts", a.ID)
}

func assertCorrectionLog(t *testing.T, f *candidateFixture, operation, objectID string) {
	t.Helper()
	f.router.GET("/api/v1/operation-logs", handler.NewOperationLogHandler(service.NewOperationLogService(f.db)).List)
	var out struct{ Data types.OperationLogPage }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet,
		"/api/v1/operation-logs?operation_type="+operation, nil, 200), &out))
	require.Len(t, out.Data.Items, 1)
	require.Equal(t, objectID, out.Data.Items[0].ObjectID)
	require.NotEmpty(t, out.Data.Items[0].Actor)
	require.NotEmpty(t, out.Data.Items[0].Before)
	require.NotEmpty(t, out.Data.Items[0].After)
}

func reprocessCorrected(t *testing.T, f *candidateFixture, entryID string) {
	t.Helper()
	r := personalRequest(t, ingestAuthHTTPRouter(f.mcpFixture), http.MethodPost,
		"/api/v1/entries/"+entryID+"/reprocess", `{}`, f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	r = personalRequest(t, ingestAuthHTTPRouter(f.mcpFixture), http.MethodGet,
		"/api/v1/entries/"+entryID, "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	var entry struct{ Data types.Entry }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &entry))
	require.True(t, entry.Data.GroupingManual)
}

func TestManualGroupingCorrectionDuringProcessingKeepsMarkerHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	understand := f.model.respond
	corrected := false
	f.model.respond = func(ctx context.Context, messages []chat.Message) string {
		if !corrected {
			corrected = true
			f.request(t, http.MethodPost, "/api/v1/facts/"+a.ID+"/move", map[string]string{"story_id": b.StoryID}, 200)
		}
		return understand(ctx, messages)
	}
	reprocessCorrected(t, f, a.Sources[0].EntryID)
	require.True(t, corrected)
	require.Len(t, groupingDetail(t, f.mcpFixture, b.StoryID).Facts, 2)
}

func TestManualGroupingResolvesCandidateMovedWhileModelRunsHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	var moved struct{ Data types.StoryFact }
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/facts/"+a.ID+"/split",
			map[string]string{"title": "Corrected target"}, 200), &moved))
		return `{"result":"same_story","confidence":0.8}`
	}
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	require.Equal(t, moved.Data.StoryID, b.StoryID)
	require.Len(t, groupingDetail(t, f.mcpFixture, b.StoryID).Facts, 2)
	require.Empty(t, groupingDetail(t, f.mcpFixture, a.StoryID).Facts)
}

func TestManualGroupingMergeSourceWinsOverMentionHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	f.model.groupRespond = func(_ context.Context, messages []chat.Message) string {
		var input struct{ Right types.Fact }
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		if input.Right.ID == a.ID {
			return `{"result":"same_occurrence","confidence":0.8}`
		}
		return `{"result":"mention","confidence":0.8}`
	}
	f.process(t, "third", candidateFact("Unlisted", "Partner"))
	f.request(t, http.MethodPost, "/api/v1/facts/"+b.ID+"/merge", map[string]string{"keep_fact_id": a.ID}, 200)
	detail := groupingDetail(t, f.mcpFixture, a.StoryID)
	require.Len(t, detail.Facts[0].Sources, 3)
	require.Empty(t, detail.Mentions, "duplicate mention cannot downgrade source evidence")
}

func TestManualGroupingRedirectChainsAndValidationHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	c := f.process(t, "third", candidateFact("Unlisted", "Partner"))
	for _, pair := range [][2]string{{a.ID, b.ID}, {b.ID, c.ID}} {
		f.request(
			t,
			http.MethodPost,
			"/api/v1/facts/"+pair[0]+"/merge",
			map[string]string{"keep_fact_id": pair[1]},
			200,
		)
	}
	for _, id := range []string{a.ID, b.ID} {
		var retained struct{ Data types.StoryFact }
		require.NoError(t, json.Unmarshal(f.request(t, http.MethodGet, "/api/v1/facts/"+id, nil, 200), &retained))
		require.Equal(t, c.ID, retained.Data.ID)
	}
	for _, pair := range [][2]string{{a.StoryID, b.StoryID}, {b.StoryID, c.StoryID}} {
		f.request(
			t,
			http.MethodPost,
			"/api/v1/stories/"+pair[0]+"/merge",
			map[string]string{"keep_story_id": pair[1]},
			200,
		)
	}
	require.Equal(t, c.StoryID, groupingDetail(t, f.mcpFixture, a.StoryID).Story.ID)
	// New AI output can still use a fact ID saved before either merge.
	item := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "ai-output", "content": "Later analysis.",
		"mentioned_fact_ids": []string{a.ID, b.ID},
	})[0]
	require.Equal(t, "accepted", item["status"])
	require.Len(t, groupingDetail(t, f.mcpFixture, c.StoryID).Mentions, 1)
	before := groupingDetail(t, f.mcpFixture, c.StoryID)
	for _, request := range []struct {
		path   string
		body   any
		status int
	}{
		{"/facts/" + c.ID + "/merge", map[string]string{"keep_fact_id": c.ID}, 400},
		{"/stories/" + c.StoryID + "/merge", map[string]string{"keep_story_id": c.StoryID}, 400},
		{"/facts/" + c.ID + "/move", map[string]string{"story_id": "missing"}, 404},
		{"/facts/" + c.ID + "/split", map[string]string{"title": " \n "}, 400},
		{"/facts/" + c.ID + "/move", map[string]string{}, 400},
	} {
		f.request(t, http.MethodPost, "/api/v1"+request.path, request.body, request.status)
	}
	r := personalRequest(
		t,
		f.router,
		http.MethodPost,
		"/api/v1/facts/"+c.ID+"/split",
		`{"title":"unauthorized"}`,
		"invalid-key",
		true,
	)
	require.Equal(t, 401, r.Code)
	require.Equal(t, before, groupingDetail(t, f.mcpFixture, c.StoryID))
}

func TestManualGroupingSplitHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"same_story","confidence":0.8}`
	}
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	require.Equal(t, a.StoryID, b.StoryID)
	var out struct{ Data types.StoryFact }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/facts/"+a.ID+"/split",
		map[string]string{"title": "Corrected story"}, 200), &out))
	require.NotEqual(t, a.StoryID, out.Data.StoryID)
	require.Len(t, groupingDetail(t, f.mcpFixture, a.StoryID).Facts, 1)
	detail := groupingDetail(t, f.mcpFixture, out.Data.StoryID)
	require.Equal(t, "Corrected story", detail.Story.Title)
	require.Equal(t, a.CreatedAt, detail.Story.LastFactAt, "correction does not create a recent fact")
	reprocessCorrected(t, f, a.Sources[0].EntryID)
	require.Equal(t, detail, groupingDetail(t, f.mcpFixture, out.Data.StoryID))
	assertCorrectionLog(t, f, "split_fact", a.ID)
}

func TestManualGroupingMergeStoriesHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	a := f.process(t, "first", candidateFact("Unlisted", "Partner"))
	b := f.process(t, "second", candidateFact("Unlisted", "Partner"))
	f.request(
		t,
		http.MethodPost,
		"/api/v1/stories/"+b.StoryID+"/merge",
		map[string]string{"keep_story_id": a.StoryID},
		200,
	)
	f.request(
		t,
		http.MethodPost,
		"/api/v1/stories/"+b.StoryID+"/merge",
		map[string]string{"keep_story_id": a.StoryID},
		200,
	)
	detail := groupingDetail(t, f.mcpFixture, b.StoryID)
	require.Equal(t, a.StoryID, detail.Story.ID)
	require.True(t, detail.Story.ManualCorrected)
	require.Len(t, detail.Facts, 2)
	for _, fact := range detail.Facts {
		require.True(t, fact.ManualCorrected)
	}
	require.Equal(t, a.CreatedAt, detail.Story.FirstFactAt)
	require.Equal(t, b.CreatedAt, detail.Story.LastFactAt)
	require.Len(t, groupingList(t, f.mcpFixture).Items, 1)
	for _, id := range []string{a.Sources[0].EntryID, b.Sources[0].EntryID} {
		reprocessCorrected(t, f, id)
	}
	require.Equal(t, detail, groupingDetail(t, f.mcpFixture, a.StoryID))
	assertCorrectionLog(t, f, "merge_stories", a.StoryID)
}
