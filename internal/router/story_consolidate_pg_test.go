package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoryConsolidateMergesDuplicateRootsAndRetainsLinksHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "root-a", "2026-10-20")
	a := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
	groupingIngest(t, f, "root-b", "2026-10-21")
	b := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
	rootCalls := 0
	model.groupRespond = func(ctx context.Context, messages []chat.Message) string {
		purpose, _ := types.LLMCallMetadataFromContext(ctx)
		if purpose == "story_consolidation" {
			rootCalls++
			var payload struct {
				Left, Right  types.Fact
				IncomingSide string `json:"incoming_side"`
			}
			require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
			if rootCalls == 1 {
				require.Equal(t, "right", payload.IncomingSide)
				require.Equal(t, a.Facts[0].ID, payload.Left.ID)
				require.Equal(t, b.Facts[0].ID, payload.Right.ID)
			} else {
				require.Equal(t, "left", payload.IncomingSide)
				require.Equal(t, b.Facts[0].ID, payload.Left.ID)
				require.Equal(t, a.Facts[0].ID, payload.Right.ID)
			}
			return `{"result":"same_story","confidence":0.9}`
		}
		var payload struct{ Right types.Fact }
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		if payload.Right.ID == a.Facts[0].ID {
			return `{"result":"same_occurrence","confidence":0.8}`
		}
		return `{"result":"same_story","confidence":0.8}`
	}
	bridge := groupingIngest(t, f, "bridge", "2026-10-22")
	runner := service.NewStoryConsolidator(f.db, nil, f.models)
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Equal(t, 2, rootCalls)
	page := groupingList(t, f)
	require.Len(t, page.Items, 1)
	require.Equal(t, a.Story.ID, page.Items[0].ID)
	detail := groupingDetail(t, f, b.Story.ID)
	require.Equal(t, a.Story.ID, detail.Story.ID)
	require.Equal(t, a.Story.FirstFactAt, detail.Story.FirstFactAt)
	require.Equal(t, b.Story.LastFactAt, detail.Story.LastFactAt)
	require.Len(t, detail.Facts, 2)
	require.Equal(t, a.Facts[0].ID, detail.Facts[0].ID)
	require.Equal(t, b.Facts[0].ID, detail.Facts[1].ID)
	require.Len(t, detail.Facts[0].Sources, 2)
	require.Equal(t, bridge, detail.Facts[0].Sources[1].EntryID)
	require.False(t, detail.Facts[0].ManualCorrected)
	require.False(t, detail.Story.ManualCorrected)
	var fromMCP types.StoryDetail
	mcpCall(t, f.connect(t, "2025-11-25"), "get_story", map[string]any{"id": b.Story.ID}, &fromMCP)
	require.Equal(t, detail, fromMCP)
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Equal(t, 2, rootCalls, "replaying the sweep must not compare an already merged pair")
}

func TestStoryRelatedRequiresTwoCurrentSourcesHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "related-root-a", "2026-10-20")
	a := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
	groupingIngest(t, f, "related-root-b", "2026-10-21")
	b := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
	tie := true
	model.groupRespond = func(ctx context.Context, messages []chat.Message) string {
		if purpose, _ := types.LLMCallMetadataFromContext(ctx); purpose == "story_consolidation" {
			return `{"result":"unrelated","confidence":0.9}`
		}
		var payload struct{ Right types.Fact }
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
		if payload.Right.ID == a.Facts[0].ID {
			return `{"result":"same_occurrence","confidence":0.8}`
		}
		if tie {
			return `{"result":"same_story","confidence":0.8}`
		}
		return `{"result":"unrelated","confidence":0.8}`
	}
	runner := service.NewStoryConsolidator(f.db, nil, f.models)
	groupingIngest(t, f, "related-bridge-a", "2026-10-22")
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Empty(t, groupingDetail(t, f, a.Story.ID).RelatedStories)
	worker := f.articles.(interfaces.EntryService)
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Empty(t, groupingDetail(t, f, a.Story.ID).RelatedStories, "a task retry is not another independent source")
	groupingIngest(t, f, "related-bridge-b", "2026-10-22")
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Equal(t, []types.Story{b.Story}, groupingDetail(t, f, a.Story.ID).RelatedStories)
	require.Equal(t, []types.Story{a.Story}, groupingDetail(t, f, b.Story.ID).RelatedStories)
	var mcpDetail types.StoryDetail
	mcpCall(t, f.connect(t, "2025-11-25"), "get_story", map[string]any{"id": a.Story.ID}, &mcpDetail)
	require.Equal(t, []types.Story{b.Story}, mcpDetail.RelatedStories)
	updated := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "related-bridge-b", "title": "Revised bridge",
		"content": "Aurora released model A. Revised bridge.", "published_at": "2026-10-22",
	})[0]
	require.Equal(t, "accepted", updated["status"])
	require.Empty(
		t,
		groupingDetail(t, f, a.Story.ID).RelatedStories,
		"withdrawal must hide the link before the next sweep",
	)
	tie = false
	require.NoError(t, worker.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Empty(
		t,
		groupingDetail(t, f, a.Story.ID).RelatedStories,
		"reattaching a revised source cannot revive its old tie",
	)
	// A new, valid judgement can establish a bridge again; withdrawing either direction removes it.
	tie = true
	groupingIngest(t, f, "related-bridge-c", "2026-10-22")
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Len(t, groupingDetail(t, f, a.Story.ID).RelatedStories, 1)
	deleteEvidenceEntry(t, f, b.Facts[0].Sources[0].EntryID)
	require.Empty(t, groupingDetail(t, f, a.Story.ID).RelatedStories, "a hidden candidate supplies no evidence")
	require.Empty(t, groupingDetail(t, f, b.Story.ID).RelatedStories)
}

func TestStoryConsolidateProtectsCorrectionsAndRechecksEvidenceHTTP(t *testing.T) {
	for _, scenario := range []string{
		"corrected_before", "corrected_during", "withdrawn_during", "bridge_withdrawn_during",
	} {
		t.Run(scenario, func(t *testing.T) {
			model := &understandingChat{respond: releaseUnderstanding}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			groupingIngest(t, f, "guard-root-a", "2026-10-20")
			a := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
			groupingIngest(t, f, "guard-root-b", "2026-10-21")
			b := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
			var bridge string
			correctedID := b.Story.ID
			correct := func() {
				r := personalRequest(t, f.router, http.MethodPost, "/api/v1/facts/"+b.Facts[0].ID+"/split",
					`{"title":"Manually separated"}`, f.key, true)
				require.Equal(t, 200, r.Code, r.Body.String())
				var out struct{ Data types.StoryFact }
				require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
				correctedID = out.Data.StoryID
			}
			rootCalls := 0
			model.groupRespond = func(ctx context.Context, messages []chat.Message) string {
				if purpose, _ := types.LLMCallMetadataFromContext(ctx); purpose == "story_consolidation" {
					rootCalls++
					if rootCalls == 1 {
						switch scenario {
						case "corrected_during":
							correct()
						case "withdrawn_during":
							deleteEvidenceEntry(t, f, b.Facts[0].Sources[0].EntryID)
						case "bridge_withdrawn_during":
							deleteEvidenceEntry(t, f, bridge)
						}
					}
					return `{"result":"same_story","confidence":0.9}`
				}
				var payload struct{ Right types.Fact }
				require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
				if payload.Right.ID == a.Facts[0].ID {
					return `{"result":"same_occurrence","confidence":0.8}`
				}
				return `{"result":"same_story","confidence":0.8}`
			}
			bridge = groupingIngest(t, f, "guard-bridge", "2026-10-22")
			if scenario == "corrected_before" {
				correct()
			}
			require.NoError(t, service.NewStoryConsolidator(f.db, nil, f.models).Consolidate(f.ctx))
			require.Equal(t, a.Story.ID, groupingDetail(t, f, a.Story.ID).Story.ID)
			detail := groupingDetail(t, f, correctedID)
			require.Equal(t, correctedID, detail.Story.ID, "neither a correction nor stale evidence may be overwritten")
			if scenario == "corrected_before" {
				require.Zero(t, rootCalls, "manual corrections are excluded before paid comparison")
			} else {
				require.Equal(t, 2, rootCalls)
			}
			if scenario == "corrected_before" || scenario == "corrected_during" {
				require.Len(t, detail.Facts, 1)
				require.Equal(t, b.Facts[0].ID, detail.Facts[0].ID)
				require.True(t, detail.Facts[0].ManualCorrected)
			}
		})
	}
}

func TestStoryConsolidateRequiresVerifiedRootTieHTTP(t *testing.T) {
	for _, scenario := range []struct {
		name, first, second string
		calls               int
	}{
		{"unrelated", `{"result":"unrelated","confidence":0.9}`, "", 1},
		{"low_confidence", `{"result":"same_story","confidence":0.84}`, "", 1},
		{"review_disagrees", `{"result":"same_story","confidence":0.9}`, `{"result":"unrelated","confidence":0.9}`, 2},
		{
			"review_uncertain", `{"result":"same_story","confidence":0.9}`,
			`{"result":"same_story","confidence":0.84}`, 2,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			model := &understandingChat{respond: releaseUnderstanding}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			groupingIngest(t, f, "review-root-a", "2026-10-20")
			a := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
			groupingIngest(t, f, "review-root-b", "2026-10-21")
			b := groupingDetail(t, f, groupingList(t, f).Items[0].ID)
			calls := 0
			model.groupRespond = func(ctx context.Context, messages []chat.Message) string {
				if purpose, _ := types.LLMCallMetadataFromContext(ctx); purpose == "story_consolidation" {
					calls++
					if calls == 1 {
						return scenario.first
					}
					return scenario.second
				}
				var payload struct{ Right types.Fact }
				require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
				if payload.Right.ID == a.Facts[0].ID {
					return `{"result":"same_occurrence","confidence":0.8}`
				}
				return `{"result":"same_story","confidence":0.8}`
			}
			groupingIngest(t, f, "review-bridge", "2026-10-22")
			require.NoError(t, service.NewStoryConsolidator(f.db, nil, f.models).Consolidate(f.ctx))
			require.Equal(t, scenario.calls, calls)
			require.Len(t, groupingList(t, f).Items, 2)
			require.Equal(t, b.Story.ID, groupingDetail(t, f, b.Story.ID).Story.ID)
		})
	}
}

func TestStoryRelatedStaysOneLevelAndFollowsMergedMembershipHTTP(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	var roots []types.StoryDetail
	for _, ref := range []string{"chain-a", "chain-b", "chain-c"} {
		groupingIngest(t, f, ref, "2026-10-20")
		roots = append(roots, groupingDetail(t, f, groupingList(t, f).Items[0].ID))
	}
	a, b, c := roots[0], roots[1], roots[2]
	runner := service.NewStoryConsolidator(f.db, nil, f.models)
	model.respond = func(context.Context, []chat.Message) string {
		return `{"title":"Discussion","summary":{"overview":"Discusses several stories."},"fact":null}`
	}
	model.groupRespond = func(context.Context, []chat.Message) string {
		return `{"result":"same_story","confidence":0.8}`
	}
	for _, ref := range []string{"discussion-a", "discussion-b"} {
		groupingIngest(t, f, ref, "2026-10-22")
	}
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Empty(t, groupingDetail(t, f, a.Story.ID).RelatedStories, "even multiple mentions are not bridge evidence")
	model.respond = releaseUnderstanding
	bridge := func(ref string, own, other types.StoryDetail) string {
		t.Helper()
		model.groupRespond = func(ctx context.Context, messages []chat.Message) string {
			if purpose, _ := types.LLMCallMetadataFromContext(ctx); purpose == "story_consolidation" {
				return `{"result":"unrelated","confidence":0.9}`
			}
			var payload struct{ Right types.Fact }
			require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &payload))
			switch payload.Right.ID {
			case own.Facts[0].ID:
				return `{"result":"same_occurrence","confidence":0.8}`
			case other.Facts[0].ID:
				return `{"result":"same_story","confidence":0.8}`
			default:
				return `{"result":"unrelated","confidence":0.8}`
			}
		}
		item := ingestRevisionHTTP(t, f, map[string]any{
			"library": "official", "source_ref": ref, "title": ref,
			"content": "Aurora released model A. " + ref, "published_at": "2026-10-22",
		})[0]
		require.Equal(t, "accepted", item["status"])
		require.NoError(t, f.articles.(interfaces.EntryService).
			ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
		return item["entry_id"].(string)
	}
	bridge("chain-ab-1", a, b)
	ab2 := bridge("chain-ab-2", a, b)
	bridge("chain-bc-1", b, c)
	bridge("chain-bc-2", b, c)
	require.NoError(t, runner.Consolidate(f.ctx))
	require.Equal(t, []types.Story{b.Story}, groupingDetail(t, f, a.Story.ID).RelatedStories)
	require.Equal(t, []types.Story{b.Story}, groupingDetail(t, f, c.Story.ID).RelatedStories)
	require.Equal(t, []types.Story{c.Story, a.Story}, groupingDetail(t, f, b.Story.ID).RelatedStories)
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/"+a.Story.ID, "", f.key, true)
	var wire struct {
		Data struct {
			Related []map[string]any `json:"related_stories"`
		}
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &wire))
	require.NotContains(t, wire.Data.Related[0], "related_stories", "neighbours must not recursively expand")
	deleteEvidenceEntry(t, f, ab2)
	require.Empty(t, groupingDetail(t, f, a.Story.ID).RelatedStories)
	require.Equal(t, []types.Story{c.Story}, groupingDetail(t, f, b.Story.ID).RelatedStories)
	bridge("chain-ab-3", a, b)
	require.NoError(t, runner.Consolidate(f.ctx))
	merge := func(from, into string) {
		t.Helper()
		body, err := json.Marshal(map[string]string{"keep_story_id": into})
		require.NoError(t, err)
		response := personalRequest(t, f.router, http.MethodPost, "/api/v1/stories/"+from+"/merge",
			string(body), f.key, true)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	merge(b.Story.ID, a.Story.ID)
	detail := groupingDetail(t, f, a.Story.ID)
	require.Equal(t, []types.Story{c.Story}, detail.RelatedStories)
	require.Equal(t, []types.Story{detail.Story}, groupingDetail(t, f, c.Story.ID).RelatedStories)
	merge(a.Story.ID, c.Story.ID)
	for _, oldID := range []string{a.Story.ID, b.Story.ID} {
		retained := groupingDetail(t, f, oldID)
		require.Equal(t, c.Story.ID, retained.Story.ID)
		require.Len(t, retained.Facts, 3)
		require.Empty(t, retained.RelatedStories, "merged events cannot link to themselves")
	}
}
