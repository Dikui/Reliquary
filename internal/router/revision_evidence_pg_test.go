package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRevisionEvidenceWithdrawsAndRestoresOriginalFactHTTP(t *testing.T) {
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	item := map[string]any{
		"library": "official", "source_id": "revision-evidence", "source_revision": 1,
		"title": "Release", "content": "Aurora released model A.", "published_at": "2026-10-20",
	}
	first := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", first["status"])
	worker := f.articles.(interfaces.EntryService)
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	before := groupingList(t, f)
	require.Len(t, before.Items, 1)
	story := before.Items[0]
	fact := groupingDetail(t, f, story.ID).Facts[0]
	item["source_revision"], item["content"] = 2, "Aurora released model A with new terms."
	updated := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", updated["status"])
	require.Equal(t, first["entry_id"], updated["entry_id"])
	require.Empty(t, groupingDetail(t, f, story.ID).Facts, "old evidence must disappear before the worker runs")
	require.Empty(t, groupingList(t, f).Items)
	model.respond = func(ctx context.Context, messages []chat.Message) string {
		return strings.Replace(releaseUnderstanding(ctx, messages),
			"Aurora released model A.", "Aurora released model A with new terms.", 1)
	}
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	detail := groupingDetail(t, f, story.ID)
	require.Len(t, detail.Facts, 1, "a hidden fact without an occurrence date must remain recallable")
	require.Equal(t, fact.ID, detail.Facts[0].ID)
	require.Equal(t, fact.CreatedAt, detail.Facts[0].CreatedAt)
	require.Equal(t, before, groupingList(t, f), "restoring evidence must preserve story IDs and recent-fact time")
	require.Len(t, detail.Facts[0].Sources, 1)
	require.Equal(t, first["entry_id"], detail.Facts[0].Sources[0].EntryID)
	require.Equal(t, "Aurora released model A with new terms.", detail.Facts[0].Sources[0].Quote)
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/facts/"+fact.ID, "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
}

func TestRevisionEvidenceSupersededProcessingCannotRestoreOldEvidenceHTTP(t *testing.T) {
	for _, phase := range []string{"understanding", "grouping"} {
		t.Run(phase, func(t *testing.T) {
			model := &understandingChat{respond: releaseUnderstanding}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			item := map[string]any{
				"library": "official", "source_id": "in-flight-revision", "source_revision": 1,
				"title": "Release", "content": "Aurora released model A.", "published_at": "2026-10-20",
			}
			worker := f.articles.(interfaces.EntryService)
			first := ingestRevisionHTTP(t, f, item)[0]
			require.Equal(t, "accepted", first["status"])
			require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
			story := groupingList(t, f).Items[0]
			fact := groupingDetail(t, f, story.ID).Facts[0]
			item["source_revision"], item["content"] = 2, "Aurora released model A. Revised report."
			require.Equal(t, "accepted", ingestRevisionHTTP(t, f, item)[0]["status"])
			olderTask := f.queue.tasks[len(f.queue.tasks)-1]
			replaced := false
			replaceRevision := func() {
				if !replaced {
					replaced = true
					// A metadata-only revision has the same body hash as the running task.
					item["source_revision"], item["published_at"] = 3, "2026-10-21"
					require.Equal(t, "accepted", ingestRevisionHTTP(t, f, item)[0]["status"])
				}
			}
			model.groupRespond = func(context.Context, []chat.Message) string {
				if phase == "grouping" {
					replaceRevision()
				}
				return `{"result":"same_occurrence","confidence":0.8}`
			}
			model.respond = func(ctx context.Context, messages []chat.Message) string {
				if phase == "understanding" {
					replaceRevision()
				}
				return releaseUnderstanding(ctx, messages)
			}
			require.NoError(t, worker.ProcessManualUpdate(f.ctx, olderTask))
			require.True(t, replaced)
			require.Empty(t, groupingDetail(t, f, story.ID).Facts, "a superseded model result cannot restore evidence")
			require.Empty(t, groupingDecisions(t, f, first["entry_id"].(string)))
			require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
			detail := groupingDetail(t, f, story.ID)
			require.Len(t, detail.Facts, 1)
			require.Equal(t, fact.ID, detail.Facts[0].ID)
		})
	}
}

func TestRevisionEvidenceFailedRevisionRollsBackEvidenceHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	entryID := groupingIngest(t, f, "rollback-evidence", "2026-10-20")
	before := groupingList(t, f)
	detail := groupingDetail(t, f, before.Items[0].ID)
	f.queue.enqueueErr = errors.New("fixture unavailable")
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "rollback-evidence", "title": "Release",
		"content": "Aurora released model A. Failed revision.", "published_at": "2026-10-20",
	})[0]
	require.Equal(t, "invalid", result["status"])
	require.Equal(t, before, groupingList(t, f))
	require.Equal(t, detail, groupingDetail(t, f, before.Items[0].ID))
	r := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodGet,
		"/api/v1/entries/"+entryID+"/body", "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Equal(t, "Aurora released model A.\n", r.Body.String())
}

func TestRevisionEvidenceDeletionKeepsManualFactWithoutSourcesHTTPAndMCP(t *testing.T) {
	f := newCandidateFixture(t)
	fact := f.process(t, "corrected", candidateFact("Unlisted", "Partner"))
	var corrected struct{ Data types.StoryFact }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/facts/"+fact.ID+"/split",
		map[string]string{"title": "Manually retained"}, 200), &corrected))
	r := personalRequest(t, ingestAuthHTTPRouter(f.mcpFixture), http.MethodDelete,
		"/api/v1/entries/"+fact.Sources[0].EntryID, "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessEntryListDelete(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	detail := groupingDetail(t, f.mcpFixture, corrected.Data.StoryID)
	require.Len(t, detail.Facts, 1, "a corrected fact remains visible without evidence")
	require.Equal(t, fact.ID, detail.Facts[0].ID)
	require.True(t, detail.Facts[0].ManualCorrected)
	require.Empty(t, detail.Facts[0].Sources)
	var wire struct {
		Facts []struct {
			NoSource bool `json:"no_source"`
		} `json:"facts"`
	}
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &wire))
	require.True(t, wire.Facts[0].NoSource)
	require.EqualValues(t, 1, groupingList(t, f.mcpFixture).Total)
	client := f.connect(t, "2025-11-25")
	var mcpDetail types.StoryDetail
	mcpCall(t, client, "get_story", map[string]any{"id": corrected.Data.StoryID}, &mcpDetail)
	require.Equal(t, detail, mcpDetail)
}

func deleteEvidenceEntry(t *testing.T, f *mcpFixture, id string) {
	t.Helper()
	r := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodDelete, "/api/v1/entries/"+id, "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessEntryListDelete(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
}

func TestRevisionEvidenceDeletionHidesFromHTTPAndMCPButKeepsRecall(t *testing.T) {
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	first := groupingIngest(t, f, "deleted-first", "2026-10-20")
	before := groupingList(t, f)
	story := before.Items[0]
	fact := groupingDetail(t, f, story.ID).Facts[0]
	second := groupingIngest(t, f, "deleted-second", "2026-10-21")
	mention := ingestRevisionHTTP(t, f, map[string]any{
		"library": "ai-output", "content": "An analysis.", "mentioned_fact_ids": []string{fact.ID},
	})[0]["entry_id"].(string)
	deleteEvidenceEntry(t, f, first)
	detail := groupingDetail(t, f, story.ID)
	require.Len(t, detail.Facts, 1)
	require.Len(t, detail.Facts[0].Sources, 1)
	require.Equal(t, second, detail.Facts[0].Sources[0].EntryID)
	deleteEvidenceEntry(t, f, second)
	detail = groupingDetail(t, f, story.ID)
	require.Empty(t, detail.Facts)
	require.Empty(t, detail.Mentions, "mentions of a hidden fact cannot leak through the story")
	require.Zero(t, groupingList(t, f).Total)
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/facts/"+fact.ID, "", f.key, true)
	require.Equal(t, 404, r.Code, r.Body.String())
	client := f.connect(t, "2025-11-25")
	var mcpPage types.StoryPage
	mcpCall(t, client, "list_stories", map[string]any{}, &mcpPage)
	require.Empty(t, mcpPage.Items)
	var mcpDetail types.StoryDetail
	mcpCall(t, client, "get_story", map[string]any{"id": story.ID}, &mcpDetail)
	require.Equal(t, detail, mcpDetail)
	thirdResult := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "replacement-source", "title": "Another report",
		"content": "Aurora released model A. Independent confirmation.", "published_at": "2026-10-22",
	})[0]
	require.Equal(t, "accepted", thirdResult["status"])
	third := thirdResult["entry_id"].(string)
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	restored := groupingDetail(t, f, story.ID)
	require.Len(t, restored.Facts, 1)
	require.Equal(t, fact.ID, restored.Facts[0].ID)
	require.Len(t, restored.Facts[0].Sources, 1)
	require.Equal(t, third, restored.Facts[0].Sources[0].EntryID)
	require.Len(t, restored.Mentions, 1)
	require.Equal(t, mention, restored.Mentions[0].EntryID)
	require.Equal(t, before, groupingList(t, f))
}

func TestRevisionEvidenceOutOfOrderRevisionAndTaskReplayHTTP(t *testing.T) {
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	item := map[string]any{
		"library": "official", "source_id": "replayed-source", "source_revision": 1,
		"title": "Release", "content": "Aurora released model A.", "published_at": "2026-10-20",
	}
	worker := f.articles.(interfaces.EntryService)
	first := ingestRevisionHTTP(t, f, item)[0]
	initialTask := f.queue.tasks[len(f.queue.tasks)-1]
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, initialTask))
	before := groupingList(t, f)
	fact := groupingDetail(t, f, before.Items[0].ID).Facts[0]
	item["source_revision"], item["content"] = 3, "Aurora released model A. Third revision."
	require.Equal(t, "accepted", ingestRevisionHTTP(t, f, item)[0]["status"])
	thirdTask := f.queue.tasks[len(f.queue.tasks)-1]
	item["source_revision"], item["content"] = 5, "Aurora released model A. Fifth revision."
	require.Equal(t, "accepted", ingestRevisionHTTP(t, f, item)[0]["status"])
	fifthTask := f.queue.tasks[len(f.queue.tasks)-1]
	item["source_revision"], item["content"] = 4, "Stale report that must never become evidence."
	stale := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "stale_revision", stale["status"])
	require.Equal(t, first["entry_id"], stale["entry_id"])
	require.Empty(t, groupingDetail(t, f, before.Items[0].ID).Facts)
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, thirdTask))
	require.Empty(t, groupingDetail(t, f, before.Items[0].ID).Facts)
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, fifthTask))
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, initialTask))
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, fifthTask))
	detail := groupingDetail(t, f, before.Items[0].ID)
	require.Len(t, detail.Facts, 1)
	require.Equal(t, fact.ID, detail.Facts[0].ID)
	require.Len(t, detail.Facts[0].Sources, 1)
	require.Equal(t, before, groupingList(t, f))
	r := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodGet,
		"/api/v1/entries/"+first["entry_id"].(string)+"/body", "", f.key, true)
	require.Equal(t, "Aurora released model A. Fifth revision.\n", r.Body.String())
}

func TestRevisionEvidenceWithdrawsInferredMentionsBeforeReprocessingHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "mention-source", "2026-10-20")
	story := groupingList(t, f).Items[0]
	model.respond = func(context.Context, []chat.Message) string {
		return `{"title":"Discussion","summary":{"overview":"Discussing a release."},"fact":null}`
	}
	model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"mention","confidence":0.8}`
	}
	mention := groupingIngest(t, f, "revised-mention", "2026-10-20")
	require.Len(t, groupingDetail(t, f, story.ID).Mentions, 1)
	item := map[string]any{
		"library": "official", "source_ref": "revised-mention", "content": "A different discussion.",
	}
	require.Equal(t, "accepted", ingestRevisionHTTP(t, f, item)[0]["status"])
	require.Empty(t, groupingDetail(t, f, story.ID).Mentions)
	model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"unrelated","confidence":0.8}`
	}
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	require.Empty(t, groupingDetail(t, f, story.ID).Mentions)
	deleteEvidenceEntry(t, f, mention)
	require.Len(t, groupingDetail(t, f, story.ID).Facts, 1)
}

func TestRevisionEvidenceContentUpdateWithdrawsBeforeReprocessingHTTP(t *testing.T) {
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	id := groupingIngest(t, f, "body-edit", "2026-10-20")
	before := groupingList(t, f)
	fact := groupingDetail(t, f, before.Items[0].ID).Facts[0]
	r := personalRequest(t, ingestAuthHTTPRouter(f), http.MethodPut, "/api/v1/entries/"+id+"/body",
		`{"content":"Aurora released model A. Edited body."}`, f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Empty(t, groupingDetail(t, f, before.Items[0].ID).Facts)
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	detail := groupingDetail(t, f, before.Items[0].ID)
	require.Len(t, detail.Facts, 1)
	require.Equal(t, fact.ID, detail.Facts[0].ID)
	require.Equal(t, before, groupingList(t, f))
}

func TestRevisionEvidenceCorrectedMaterialLosesEvidenceButKeepsProtectionHTTP(t *testing.T) {
	f := newCandidateFixture(t)
	fact := f.process(t, "manual-revision", candidateFact("Unlisted", "Partner"))
	var corrected struct{ Data types.StoryFact }
	require.NoError(t, json.Unmarshal(f.request(t, http.MethodPost, "/api/v1/facts/"+fact.ID+"/split",
		map[string]string{"title": "Retained correction"}, 200), &corrected))
	item := ingestRevisionHTTP(t, f.mcpFixture, map[string]any{
		"library": "official", "source_ref": "manual-revision", "title": "Corrected report",
		"content": "A collaboration was announced. Updated text.", "published_at": "2026-10-20",
	})[0]
	require.Equal(t, "accepted", item["status"])
	detail := groupingDetail(t, f.mcpFixture, corrected.Data.StoryID)
	require.Len(t, detail.Facts, 1)
	require.Empty(t, detail.Facts[0].Sources)
	require.True(t, detail.Facts[0].NoSource)
	f.model.groupRespond = func(context.Context, []chat.Message) string {
		t.Fatal("manual grouping must remain protected after source revision")
		return ""
	}
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	require.Equal(t, detail, groupingDetail(t, f.mcpFixture, corrected.Data.StoryID))
	f.request(t, http.MethodGet, "/api/v1/entries/"+item["entry_id"].(string), nil, 200)
}

func TestRevisionEvidenceUnchangedRevisionDoesNotStrandRunningUnderstandingHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	item := map[string]any{
		"library": "official", "source_id": "unchanged-running", "source_revision": 1,
		"title": "Release", "content": "Aurora released model A.", "published_at": "2026-10-20",
	}
	first := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", first["status"])
	advanced := false
	model.respond = func(ctx context.Context, messages []chat.Message) string {
		if !advanced {
			advanced = true
			item["source_revision"] = 2
			require.Equal(t, "exists", ingestRevisionHTTP(t, f, item)[0]["status"])
		}
		return releaseUnderstanding(ctx, messages)
	}
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	require.True(t, advanced)
	page := groupingList(t, f)
	require.Len(t, page.Items, 1, "an unchanged revision keeps the existing processing operation usable")
	require.Len(t, groupingDetail(t, f, page.Items[0].ID).Facts, 1)
}

func TestRevisionEvidenceHiddenRecallKeepsOriginalDateWindowHTTP(t *testing.T) {
	for _, published := range []string{"2026-11-03", "2026-11-04"} {
		t.Run(published, func(t *testing.T) {
			model := &understandingChat{
				respond: releaseUnderstanding,
				groupRespond: func(context.Context, []chat.Message) string {
					return `{"result":"same_occurrence","confidence":0.8}`
				},
			}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			entry := groupingIngest(t, f, "withdrawn-date", "2026-10-20")
			story := groupingList(t, f).Items[0]
			deleteEvidenceEntry(t, f, entry)
			item := ingestRevisionHTTP(t, f, map[string]any{
				"library": "official", "source_ref": "dated-replacement", "title": "New confirmation",
				"content": "Aurora released model A. Another confirmation.", "published_at": published,
			})[0]
			require.Equal(t, "accepted", item["status"])
			require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
				f.queue.tasks[len(f.queue.tasks)-1]))
			page := groupingList(t, f)
			require.Len(t, page.Items, 1)
			if published == "2026-11-03" {
				require.Equal(t, story, page.Items[0])
			} else {
				require.NotEqual(t, story.ID, page.Items[0].ID, "hidden facts still obey the date window")
			}
		})
	}
}
