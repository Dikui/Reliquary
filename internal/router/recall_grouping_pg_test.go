package router

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func groupingList(t *testing.T, f *mcpFixture) types.StoryPage {
	t.Helper()
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories", "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	var out struct{ Data types.StoryPage }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	return out.Data
}

func groupingDetail(t *testing.T, f *mcpFixture, id string) types.StoryDetail {
	t.Helper()
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/"+id, "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	var out struct{ Data types.StoryDetail }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	return out.Data
}

func groupingIngest(t *testing.T, f *mcpFixture, ref, date string) string {
	t.Helper()
	item := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": ref, "title": ref,
		"content": "Aurora released model A.", "published_at": date,
	})[0]
	require.Equal(t, "accepted", item["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	return item["entry_id"].(string)
}

func releaseUnderstanding(context.Context, []chat.Message) string {
	return `{"title":"发布资料","summary":{"overview":"机构发布模型。"},
"fact":{"title":"模型发布","subject":"Aurora","action":"发布","object":"model A",
"quote":"Aurora released model A."}}`
}

func TestRecallGroupingSameOccurrenceKeepsFactAndRecentTime(t *testing.T) {
	calls := 0
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			calls++
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	first := groupingIngest(t, f, "first", "2026-10-20")
	before := groupingList(t, f)
	require.Len(t, before.Items, 1)
	require.Zero(t, calls, "no candidates must not call the grouping model")
	original := groupingDetail(t, f, before.Items[0].ID)
	second := groupingIngest(t, f, "second", "2026-10-21")
	require.Equal(t, 1, calls)
	require.Equal(t, before, groupingList(t, f))
	detail := groupingDetail(t, f, before.Items[0].ID)
	require.Len(t, detail.Facts, 1)
	require.Equal(t, original.Facts[0].ID, detail.Facts[0].ID)
	require.Len(t, detail.Facts[0].Sources, 2)
	require.Equal(t, first, detail.Facts[0].Sources[0].EntryID)
	require.Equal(t, second, detail.Facts[0].Sources[1].EntryID)
	require.Equal(t, "Aurora released model A.", detail.Facts[0].Sources[1].Quote)
}

func TestRecallGroupingFourResultsAndDateRules(t *testing.T) {
	for _, scenario := range []struct {
		name, result, published, occurred string
		confidence                        float64
		stories, facts, sources, calls    int
	}{
		{"continuation", "same_story", "2026-10-21", "", 0.8, 1, 2, 1, 1},
		{"unrelated", "unrelated", "2026-10-21", "", 0.8, 2, 1, 1, 1},
		{"mention", "mention", "2026-10-21", "", 0.8, 1, 1, 1, 1},
		{"window_boundary", "same_occurrence", "2026-11-03", "", 0.8, 1, 1, 2, 1},
		{"outside_window", "same_occurrence", "2026-11-04", "", 0.8, 2, 1, 1, 0},
		{"occurrence_window", "same_occurrence", "2026-12-20", "2026-10-21", 0.8, 1, 1, 2, 1},
		{"no_dates_low_confidence", "same_occurrence", "", "", 0.84, 1, 1, 1, 1},
		{"no_dates_high_confidence", "same_occurrence", "", "", 0.85, 1, 1, 2, 2},
		{"no_dates_continuation", "same_story", "", "", 0.95, 1, 1, 1, 2},
		{"no_dates_unrelated", "unrelated", "", "", 0.95, 1, 1, 1, 2},
		{"occurrence_without_publication_continues", "same_story", "", "2026-10-21", 0.8, 1, 2, 1, 1},
		{"occurrence_without_publication_cannot_start_story", "unrelated", "", "2026-10-21", 0.8, 1, 1, 1, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			occurrence := ""
			model := &understandingChat{respond: func(context.Context, []chat.Message) string {
				raw, err := json.Marshal(
					map[string]any{
						"title":   "发布资料",
						"summary": map[string]string{"overview": "机构发布模型。"},
						"fact": map[string]string{
							"title": "模型发布", "subject": "Aurora", "action": "发布", "object": "model A",
							"quote": "Aurora released model A.", "occurred_at": occurrence,
						},
					},
				)
				require.NoError(t, err)
				return string(raw)
			}, groupRespond: func(context.Context, []chat.Message) string {
				calls++
				raw, err := json.Marshal(map[string]any{"result": scenario.result, "confidence": scenario.confidence})
				require.NoError(t, err)
				return string(raw)
			}}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			groupingIngest(t, f, "first", "2026-10-20")
			before := groupingList(t, f)
			original := groupingDetail(t, f, before.Items[0].ID)
			occurrence = scenario.occurred
			groupingIngest(t, f, "next", scenario.published)
			page := groupingList(t, f)
			require.Len(t, page.Items, scenario.stories)
			detail := groupingDetail(t, f, before.Items[0].ID)
			require.Len(t, detail.Facts, scenario.facts)
			var retained *types.StoryFact
			for i := range detail.Facts {
				if detail.Facts[i].ID == original.Facts[0].ID {
					retained = &detail.Facts[i]
				}
			}
			require.NotNil(t, retained)
			require.Len(t, retained.Sources, scenario.sources)
			require.Equal(t, scenario.calls, calls)
			if scenario.facts == 1 {
				require.Equal(t, before.Items[0], detail.Story)
			} else {
				require.True(t, detail.Story.LastFactAt.After(before.Items[0].LastFactAt))
			}
		})
	}
}

func TestRecallGroupingUndatedWithoutCandidatesDoesNotStartStory(t *testing.T) {
	calls := 0
	model := &understandingChat{
		respond:      releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string { calls++; return "" },
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "undated", "")
	require.Empty(t, groupingList(t, f).Items)
	require.Zero(t, calls)
}

func groupingDecisions(t *testing.T, f *mcpFixture, id string) []types.GroupingDecision {
	t.Helper()
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/entries/"+id+"/grouping-decisions", "", f.key, true)
	require.Equal(t, 200, r.Code, r.Body.String())
	var out struct{ Data []types.GroupingDecision }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &out))
	return out.Data
}

func TestRecallGroupingSwappedReviewAndAudit(t *testing.T) {
	for _, review := range []string{"same_occurrence", "same_story", "unrelated"} {
		t.Run(review, func(t *testing.T) {
			calls := 0
			model := &understandingChat{
				respond: releaseUnderstanding,
				groupRespond: func(_ context.Context, messages []chat.Message) string {
					calls++
					var input map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
					result := "same_occurrence"
					if calls == 1 {
						require.JSONEq(t, `"left"`, string(input["incoming_side"]))
					} else {
						require.JSONEq(t, `"right"`, string(input["incoming_side"]))
						require.Contains(t, string(input["right"]), `"fact"`)
						require.Contains(t, string(input["left"]), `"story_id"`)
						result = review
					}
					raw, err := json.Marshal(map[string]any{"result": result, "confidence": 0.85})
					require.NoError(t, err)
					return string(raw)
				},
			}
			f := newMCPFixture(t, mcpProcessingFixture{chat: model})
			f.seedProcessingModels(t)
			first := groupingIngest(t, f, "first", "2026-10-20")
			require.Empty(t, groupingDecisions(t, f, first))
			original := groupingList(t, f).Items[0]
			second := groupingIngest(t, f, "second", "2026-10-21")
			require.Equal(t, 2, calls)
			decisions := groupingDecisions(t, f, second)
			require.Len(t, decisions, 1)
			decision := decisions[0]
			require.InDelta(t, 1, decision.Similarity, 0.00001)
			require.InDelta(t, 0.85, decision.Confidence, 0.00001)
			require.NotNil(t, decision.ReviewConfidence)
			require.Equal(t, "same_occurrence", decision.Result)
			require.Equal(t, review, decision.ReviewResult)
			require.Equal(t, "mcp-chat", decision.ModelID)
			if review == "same_occurrence" {
				require.Equal(t, "same_occurrence", decision.AppliedResult)
				require.Len(t, groupingList(t, f).Items, 1)
				require.Len(t, groupingDetail(t, f, original.ID).Facts[0].Sources, 2)
			} else {
				require.Equal(t, "unrelated", decision.AppliedResult)
				require.Contains(t, decision.Reason, "review_disagreed")
				require.Len(t, groupingList(t, f).Items, 2)
				require.Len(t, groupingDetail(t, f, original.ID).Facts[0].Sources, 1)
			}
		})
	}
}

type groupingEmbedder struct {
	mcpFixtureEmbedder
	score float32
}

func (e *groupingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if strings.Contains(text, "QueryMarker") {
		return []float32{e.score, float32(math.Sqrt(1 - float64(e.score)*float64(e.score))), 0}, nil
	}
	return []float32{1, 0, 0}, nil
}

func TestRecallGroupingSimilarityAndConfig(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		score  float32
		config *config.FactGroupingConfig
		calls  int
	}{
		{"default_below", 0.59, nil, 0},
		{"default_at", 0.6, nil, 1},
		{"configured_recall", 0.7, &config.FactGroupingConfig{RecallThreshold: 0.75}, 0},
		{"configured_review", 0.7, &config.FactGroupingConfig{ReviewThreshold: 0.75}, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			query := false
			model := &understandingChat{respond: func(ctx context.Context, m []chat.Message) string {
				text := releaseUnderstanding(ctx, m)
				if query {
					return strings.Replace(text, "模型发布", "QueryMarker", 1)
				}
				return text
			}, groupRespond: func(context.Context, []chat.Message) string {
				calls++
				return `{"result":"same_occurrence","confidence":0.8}`
			}}
			f := newMCPFixture(
				t,
				mcpProcessingFixture{
					chat:     model,
					embedder: &groupingEmbedder{score: scenario.score},
					grouping: scenario.config,
				},
			)
			f.seedProcessingModels(t)
			groupingIngest(t, f, "first", "2026-10-20")
			query = true
			second := groupingIngest(t, f, "query", "2026-10-21")
			require.Equal(t, scenario.calls, calls)
			if calls == 0 {
				require.Len(t, groupingList(t, f).Items, 2)
				require.Empty(t, groupingDecisions(t, f, second))
			} else {
				require.Len(t, groupingList(t, f).Items, 1)
				require.InDelta(t, float64(scenario.score), groupingDecisions(t, f, second)[0].Similarity, 0.0001)
			}
		})
	}
}

func TestRecallGroupingSynthesisAndAIOnlyMention(t *testing.T) {
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			// Even a bad model cannot promote a material without a qualified fact to a source.
			return `{"result":"same_occurrence","confidence":0.8}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	first := groupingIngest(t, f, "first", "2026-10-20")
	original := groupingList(t, f).Items[0]
	model.respond = func(context.Context, []chat.Message) string {
		return `{"title":"综合讨论","summary":{"overview":"本篇讨论模型发布。"},"fact":null}`
	}
	var mentions []string
	for _, library := range []string{"official", "references", "personal", "ai-output"} {
		item := ingestRevisionHTTP(t, f, map[string]any{
			"library": library, "source_ref": "discussion-" + library, "title": "Discussion",
			"content": "Discussing Aurora released model A.", "published_at": "2026-10-20",
		})[0]
		require.Equal(t, "accepted", item["status"])
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
		mentions = append(mentions, item["entry_id"].(string))
	}
	detail := groupingDetail(t, f, original.ID)
	require.Len(t, detail.Facts, 1)
	require.Len(t, detail.Facts[0].Sources, 1)
	require.Equal(t, first, detail.Facts[0].Sources[0].EntryID)
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	for _, id := range mentions {
		require.Contains(t, string(raw), id)
	}
	require.Equal(t, original, detail.Story)
}

func TestRecallGroupingAIExplicitFactMentionsHTTPAndMCP(t *testing.T) {
	calls := 0
	model := &understandingChat{
		respond: releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string {
			calls++
			return `{"result":"unrelated","confidence":0.7}`
		},
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "first", "2026-10-20")
	original := groupingList(t, f).Items[0]
	fact := groupingDetail(t, f, original.ID).Facts[0]
	item := map[string]any{
		"library":            "ai-output",
		"source_ref":         "ai-explicit",
		"title":              "Analysis",
		"content":            "An external analysis.",
		"mentioned_fact_ids": []string{fact.ID, fact.ID},
	}
	result := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	require.Zero(t, calls, "explicit mentions do not need model inference")
	detail := groupingDetail(t, f, original.ID)
	require.Len(t, detail.Facts[0].Sources, 1)
	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	require.Contains(t, string(raw), result["entry_id"])
	for _, invalid := range []map[string]any{
		{"library": "ai-output", "content": "Invalid target.", "mentioned_fact_ids": []string{"missing"}},
		{"library": "official", "content": "Not an AI output.", "mentioned_fact_ids": []string{fact.ID}},
	} {
		outcome := ingestRevisionHTTP(t, f, invalid)[0]
		require.Equal(t, "invalid", outcome["status"], outcome)
	}
	client := f.connect(t, "2025-11-25")
	var mcpResult types.IngestBatchResult
	mcpCall(
		t,
		client,
		"ingest",
		map[string]any{
			"items": []map[string]any{
				{
					"library":            "ai-output",
					"source_ref":         "mcp-explicit",
					"content":            "MCP analysis.",
					"mentioned_fact_ids": []string{fact.ID},
				},
			},
		},
		&mcpResult,
	)
	require.Equal(t, "accepted", mcpResult.Items[0].Status)
	var mcpDetail types.StoryDetail
	mcpCall(t, client, "get_story", map[string]any{"id": original.ID}, &mcpDetail)
	raw, err = json.Marshal(mcpDetail)
	require.NoError(t, err)
	require.Contains(t, string(raw), mcpResult.Items[0].EntryID)
	require.Equal(t, original, mcpDetail.Story)
	require.Len(t, mcpDetail.Facts[0].Sources, 1)
}

func TestRecallGroupingExplicitMentionsCanBeRetainedAndCleared(t *testing.T) {
	model := &understandingChat{respond: releaseUnderstanding}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "first", "2026-10-20")
	story := groupingList(t, f).Items[0]
	fact := groupingDetail(t, f, story.ID).Facts[0]
	item := map[string]any{
		"library": "ai-output", "source_ref": "editable-analysis", "title": "Analysis",
		"content": "Initial analysis.", "mentioned_fact_ids": []string{fact.ID},
	}
	first := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", first["status"])
	require.Len(t, groupingDetail(t, f, story.ID).Mentions, 1)
	delete(item, "mentioned_fact_ids")
	item["content"] = "Updated analysis."
	second := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", second["status"])
	require.Equal(t, first["entry_id"], second["entry_id"])
	require.Empty(t, groupingDetail(t, f, story.ID).Mentions, "revision withdraws the old mention immediately")
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx,
		f.queue.tasks[len(f.queue.tasks)-1]))
	require.Len(t, groupingDetail(t, f, story.ID).Mentions, 1)
	item["mentioned_fact_ids"] = []string{}
	third := ingestRevisionHTTP(t, f, item)[0]
	require.Equal(t, "accepted", third["status"])
	require.Equal(t, first["entry_id"], third["entry_id"])
	require.Empty(t, groupingDetail(t, f, story.ID).Mentions)
	require.Equal(t, story, groupingDetail(t, f, story.ID).Story)
}

func TestRecallGroupingMalformedDecisionKeepsEvidenceUnchanged(t *testing.T) {
	model := &understandingChat{
		respond:      releaseUnderstanding,
		groupRespond: func(context.Context, []chat.Message) string { return `{"result":"same_occurrence"}` },
	}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	groupingIngest(t, f, "first", "2026-10-20")
	before := groupingList(t, f)
	item := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "invalid-decision",
		"title": "Release", "content": "Aurora released model A.", "published_at": "2026-10-21",
	})[0]
	require.Equal(t, "accepted", item["status"])
	err := f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1])
	require.ErrorContains(t, err, "invalid fact grouping confidence")
	require.Equal(t, before, groupingList(t, f))
	require.Len(t, groupingDetail(t, f, before.Items[0].ID).Facts[0].Sources, 1)
	require.Empty(t, groupingDecisions(t, f, item["entry_id"].(string)))
}
