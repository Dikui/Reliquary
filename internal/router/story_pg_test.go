package router

import (
	"context"
	"encoding/json"
	"net/http"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoryFromTimelyMaterialHTTPAndMCP(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	model.respond = func(_ context.Context, messages []chat.Message) string {
		require.Contains(t, messages[0].Content, "综合稿")
		return `{"title":"发布资料",
"summary":{"overview":"机构发布了新模型。"},
"fact":{"title":"新模型发布",
"subject":"Aurora",
"action":"发布",
"object":"模型 A",
"conditions":"公开预览",
"occurred_at":"2026-10-20",
"quote":"Aurora released model A."}}`
	}
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "story-release", "title": "Release",
		"content": "# Release\n\nAurora released model A.", "published_at": "2026-10-20",
	})[0]
	require.Equal(t, "accepted", result["status"])
	id := result["entry_id"].(string)
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	read := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories", "", f.key, true)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	var listed struct {
		Data struct {
			Items []struct{ ID, Title string }
			Total int
		}
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &listed))
	require.Equal(t, 1, listed.Data.Total)
	require.Len(t, listed.Data.Items, 1)
	storyID := listed.Data.Items[0].ID
	require.NotEmpty(t, storyID)
	require.Equal(t, "新模型发布", listed.Data.Items[0].Title)
	var detail struct {
		Data struct {
			Story struct{ ID string }
			Facts []struct {
				ID, Title, Subject, Action, Object, Conditions string
				OccurredAt                                     string `json:"occurred_at"`
				Sources                                        []struct {
					EntryID            string `json:"entry_id"`
					Title, Quote, Role string
					Manual             bool
				}
			}
		}
	}
	read = personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/"+storyID, "", f.key, true)
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &detail))
	require.Equal(t, storyID, detail.Data.Story.ID)
	require.Len(t, detail.Data.Facts, 1)
	fact := detail.Data.Facts[0]
	require.NotEmpty(t, fact.ID)
	require.Equal(t, "新模型发布", fact.Title)
	require.Equal(t, "Aurora", fact.Subject)
	require.Equal(t, "发布", fact.Action)
	require.Equal(t, "模型 A", fact.Object)
	require.Equal(t, "公开预览", fact.Conditions)
	require.Contains(t, fact.OccurredAt, "2026-10-20")
	require.Len(t, fact.Sources, 1)
	require.Equal(t, id, fact.Sources[0].EntryID)
	require.Equal(t, "发布资料", fact.Sources[0].Title)
	require.Equal(t, "Aurora released model A.", fact.Sources[0].Quote)
	require.Equal(t, "source", fact.Sources[0].Role)
	require.False(t, fact.Sources[0].Manual)
	client := f.connect(t, "2025-11-25")
	var mcpList struct {
		Items []struct{ ID string }
		Total int
	}
	mcpCall(t, client, "list_stories", map[string]any{}, &mcpList)
	require.Equal(t, storyID, mcpList.Items[0].ID)
	var mcpDetail struct {
		Story struct{ ID string }
		Facts []struct{ ID string }
	}
	mcpCall(t, client, "get_story", map[string]any{"id": storyID}, &mcpDetail)
	require.Equal(t, storyID, mcpDetail.Story.ID)
	require.Equal(t, fact.ID, mcpDetail.Facts[0].ID)
}

func TestStoryRejectsInvalidEvidenceAndOtherTemplates(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	quote := "Aurora released model A."
	model.respond = func(_ context.Context, messages []chat.Message) string {
		var input struct {
			Title       string
			FactAllowed bool `json:"fact_allowed"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		if input.Title == "Synthesis" {
			require.Contains(t, messages[0].Content, "综合稿")
			return `{"title":"综合稿","summary":{"overview":"本篇综合已有报道。"},"fact":null}`
		}
		fact := map[string]any{"title": "新模型发布", "subject": "Aurora", "action": "发布", "object": "模型 A", "quote": quote}
		raw, err := json.Marshal(
			map[string]any{"title": "理解资料", "summary": map[string]string{"overview": "已理解资料。"}, "fact": fact},
		)
		require.NoError(t, err)
		return string(raw)
	}
	for _, scenario := range []struct{ library, title, body, quote string }{
		{"official", "Changed quotation", "Aurora released model A.", "Aurora released model B."},
		{"official", "Rewritten quotation", "Aurora released model A.", "Aurora  released model A."},
		{"official", "Numbered quotation", "Aurora released model A.", "[P1]\nAurora released model A."},
		{"official", "Empty quotation", "Aurora released model A.", ""},
		{"official", "Synthesis", "Aurora released model A. Other sources disagree.", "Aurora released model A."},
		{"references", "Reference", "Aurora released model A.", "Aurora released model A."},
		{"personal", "Personal", "Aurora released model A.", "Aurora released model A."},
		{"ai-output", "Generated", "Aurora released model A.", "Aurora released model A."},
	} {
		t.Run(scenario.title, func(t *testing.T) {
			quote = scenario.quote
			result := ingestRevisionHTTP(t, f, map[string]any{
				"library": scenario.library, "source_ref": scenario.title, "title": scenario.title,
				"content": scenario.body, "published_at": "2026-10-20",
			})[0]
			require.Equal(t, "accepted", result["status"])
			require.NoError(
				t,
				f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
			)
			read := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories", "", f.key, true)
			require.Equal(t, 200, read.Code, read.Body.String())
			var page struct {
				Data struct {
					Items []any
					Total int
				}
			}
			require.NoError(t, json.Unmarshal(read.Body.Bytes(), &page))
			require.Zero(t, page.Data.Total)
			require.Empty(t, page.Data.Items)
			entry := personalRequest(
				t,
				ingestAuthHTTPRouter(f),
				http.MethodGet,
				"/api/v1/entries/"+result["entry_id"].(string),
				"",
				f.key,
				true,
			)
			require.Equal(t, 200, entry.Code)
			require.Contains(t, entry.Body.String(), `"summary_status":"completed"`)
			if scenario.library == "ai-output" {
				require.Contains(t, entry.Body.String(), `"cannot_be_fact":true`)
			}
		})
	}
}

func TestStoryLongMaterialSelectsOneWholeMaterialFact(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model, maxBodyChars: 900})
	f.seedProcessingModels(t)
	model.respond = func(ctx context.Context, messages []chat.Message) string {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "article_summary_merge" {
			return `{"title":"整篇发布资料",
"summary":{"overview":"整篇报道新模型发布。"},
"fact":{"title":"长文中的发布",
"subject":"Aurora",
"action":"发布",
"object":"模型 A",
"quote":"Aurora released model A."}}`
		}
		if strings.Contains(messages[1].Content, "Aurora released model A.") {
			return `{"title":"发布段落",
"summary":{"overview":"本段报道发布。"},
"fact":{"title":"长文中的发布",
"subject":"Aurora",
"action":"发布",
"object":"模型 A",
"quote":"Aurora released model A."}}`
		}
		return `{"title":"分段理解","summary":{"overview":"本段为背景介绍。"},"fact":null}`
	}
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library": "official", "source_ref": "long-fact", "title": "Long material",
		"content":      strings.Repeat("Prior context and measurements.\n\n", 70) + "Aurora released model A.",
		"published_at": "2026-10-20",
	})[0]
	require.Equal(t, "accepted", result["status"])
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	read := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories", "", f.key, true)
	require.Equal(t, 200, read.Code, read.Body.String())
	var page struct {
		Data struct {
			Items []struct{ ID, Title string }
			Total int
		}
	}
	require.NoError(t, json.Unmarshal(read.Body.Bytes(), &page))
	require.Equal(t, 1, page.Data.Total)
	require.Len(t, page.Data.Items, 1)
	require.Equal(t, "长文中的发布", page.Data.Items[0].Title)
}

func TestStoryPaginationUsesNewFactTimeAndReprocessingKeepsOneSource(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	occurrence := "2026-10-22"
	model.respond = func(_ context.Context, _ []chat.Message) string {
		raw, err := json.Marshal(map[string]any{
			"title": "发布资料", "summary": map[string]string{"overview": "机构发布模型。"},
			"fact": map[string]any{
				"title":       occurrence,
				"subject":     "Aurora",
				"action":      "发布",
				"quote":       "Aurora released a model.",
				"occurred_at": occurrence,
			},
		})
		require.NoError(t, err)
		return string(raw)
	}
	var entryIDs []string
	for _, ref := range []string{"new-occurrence", "older-occurrence"} {
		result := ingestRevisionHTTP(t, f, map[string]any{
			"library": "official", "source_ref": ref, "title": ref, "published_at": "2026-10-23",
			"content": "Aurora released a model.",
		})[0]
		require.Equal(t, "accepted", result["status"])
		entryIDs = append(entryIDs, result["entry_id"].(string))
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
		occurrence = "2026-10-01"
	}
	list := func(query string) types.StoryPage {
		t.Helper()
		read := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories"+query, "", f.key, true)
		require.Equal(t, 200, read.Code, read.Body.String())
		var out struct{ Data types.StoryPage }
		require.NoError(t, json.Unmarshal(read.Body.Bytes(), &out))
		return out.Data
	}
	page := list("?limit=1")
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, "2026-10-01", page.Items[0].Title)
	olderPage := list("?limit=1&offset=1")
	require.Equal(t, "2026-10-22", olderPage.Items[0].Title)
	require.True(t, page.Items[0].LastFactAt.After(olderPage.Items[0].LastFactAt))
	before := list("")
	r := ingestAuthHTTPRouter(f)
	response := personalRequest(t, r, http.MethodPost, "/api/v1/entries/"+entryIDs[0]+"/reprocess", `{}`, f.key, true)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	require.Equal(t, before, list(""))
	var detail struct{ Data types.StoryDetail }
	response = personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/"+olderPage.Items[0].ID, "", f.key, true)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &detail))
	require.Len(t, detail.Data.Facts, 1)
	require.Len(t, detail.Data.Facts[0].Sources, 1)
	require.Equal(t, entryIDs[0], detail.Data.Facts[0].Sources[0].EntryID)
	var mcpPage types.StoryPage
	mcpCall(t, f.connect(t, "2025-11-25"), "list_stories", map[string]any{"limit": 1, "offset": 1}, &mcpPage)
	require.Equal(t, olderPage, mcpPage)
	for _, path := range []string{"?limit=101", "?offset=-1", "?limit=bad"} {
		response = personalRequest(t, f.router, http.MethodGet, "/api/v1/stories"+path, "", f.key, true)
		require.Equal(t, 400, response.Code, response.Body.String())
	}
	response = personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/missing", "", f.key, true)
	require.Equal(t, 404, response.Code, response.Body.String())
}

func TestStoryOccurrencePrecisionHTTPAndMCP(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	client := f.connect(t, "2025-11-25")
	var occurrence string
	model.respond = func(_ context.Context, _ []chat.Message) string {
		raw, err := json.Marshal(map[string]any{
			"title": "发布资料", "summary": map[string]string{"overview": "机构发布模型。"},
			"fact": map[string]string{
				"title": "模型发布", "subject": "Aurora", "action": "发布",
				"quote": "Aurora released a model.", "occurred_at": occurrence,
			},
		})
		require.NoError(t, err)
		return string(raw)
	}
	readList := func() types.StoryPage {
		t.Helper()
		response := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories", "", f.key, true)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var page struct{ Data types.StoryPage }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		return page.Data
	}
	previous := []types.Story{}
	for _, scenario := range []struct{ name, value string }{
		{"date", "2026-10-20"},
		{"timestamp", "2026-10-19T08:30:12.345+08:00"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			occurrence = scenario.value
			result := ingestRevisionHTTP(t, f, map[string]any{
				"library": "official", "source_ref": "occurrence-" + scenario.name,
				"title": "Release", "content": "Aurora released a model.", "published_at": "2026-10-23",
			})[0]
			require.Equal(t, "accepted", result["status"])
			require.NoError(t, f.articles.(interfaces.EntryService).
				ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
			page := readList()
			require.Len(t, page.Items, len(previous)+1)
			require.Equal(t, previous, page.Items[1:])
			previous = page.Items
			id := page.Items[0].ID
			type detailWire struct {
				Story types.Story `json:"story"`
				Facts []struct {
					OccurredAt string `json:"occurred_at"`
				} `json:"facts"`
			}
			var httpDetail struct{ Data detailWire }
			response := personalRequest(t, f.router, http.MethodGet, "/api/v1/stories/"+id, "", f.key, true)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &httpDetail))
			require.Len(t, httpDetail.Data.Facts, 1)
			require.Equal(t, scenario.value, httpDetail.Data.Facts[0].OccurredAt)
			var mcpDetail detailWire
			mcpCall(t, client, "get_story", map[string]any{"id": id}, &mcpDetail)
			require.Equal(t, httpDetail.Data, mcpDetail)
			var mcpPage types.StoryPage
			mcpCall(t, client, "list_stories", map[string]any{}, &mcpPage)
			require.Equal(t, page, mcpPage)
			require.Equal(t, page, readList(), "reading occurrence times cannot change story ordering or timestamps")
		})
	}
}
