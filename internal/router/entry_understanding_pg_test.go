package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/modelcalls"
	"reliquary/internal/models/chat"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/stretchr/testify/require"
)

type understandingChat struct {
	mcpFixtureChat
	respond      func(context.Context, []chat.Message) string
	groupRespond func(context.Context, []chat.Message) string
}

func (m *understandingChat) Chat(
	ctx context.Context,
	messages []chat.Message,
	_ *chat.ChatOptions,
) (*types.ChatResponse, error) {
	if purpose, _ := types.LLMCallMetadataFromContext(ctx); purpose == "fact_grouping" ||
		purpose == "story_consolidation" {
		if m.groupRespond != nil {
			return &types.ChatResponse{Content: m.groupRespond(ctx, messages)}, nil
		}
		return &types.ChatResponse{Content: `{"result":"unrelated","confidence":0.7}`}, nil
	}
	return &types.ChatResponse{Content: m.respond(ctx, messages)}, nil
}

func TestEntryUnderstandingAllLibrariesValidateChoices(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	call := func(method, path, body string) []byte {
		t.Helper()
		r := personalRequest(t, f.router, method, path, body, f.key, true)
		require.Less(t, r.Code, 300, r.Body.String())
		return r.Body.Bytes()
	}
	var domains struct{ Data []types.Domain }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/domains", ""), &domains))
	var entity struct{ Data types.Entity }
	require.NoError(
		t,
		json.Unmarshal(
			call(
				http.MethodPost,
				"/api/v1/entities",
				`{"canonical_name":"理解测试机构","entity_type":"organization","aliases":["Understanding Inc"]}`,
			),
			&entity,
		),
	)
	model.respond = func(_ context.Context, messages []chat.Message) string {
		var input map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		require.Contains(t, string(input["domain_options"]), domains.Data[0].Definition)
		require.Contains(t, string(input["entity_options"]), entity.Data.ID)
		require.Contains(t, string(input["entity_options"]), "Understanding Inc")
		require.Contains(t, messages[0].Content, "中文")
		require.NotContains(t, messages[0].Content, "本库保存", "all libraries use the common prompt without retired library rules")
		return fmt.Sprintf(`{"title":"中文资料标题",
"summary":{"overview":"这是一句话概述。",
"points":["discard"],
"key_terms":["discard"]},
"domain_ids":["missing",
%q,
%q,
42],
"entity_ids":["missing",
%q,
%q,
null],"entity_mentions":[{"name":"Understanding Inc","type":"organization","entity_id":%q,"quote":"Understanding Inc recorded original source measurements."},{"name":"Unlisted Research Institute","type":"organization","quote":"Unlisted Research Institute contributed data."}]}`, domains.Data[0].ID, domains.Data[0].ID, entity.Data.ID, entity.Data.ID, entity.Data.ID)
	}
	r := ingestAuthHTTPRouter(f)
	for _, library := range []string{"official", "references", "personal", "ai-output"} {
		t.Run(library, func(t *testing.T) {
			result := ingestRevisionHTTP(t, f, map[string]any{
				"library":    library,
				"source_ref": "stable-" + library,
				"title":      "English source title",
				"content":    "Understanding Inc recorded original source measurements. Unlisted Research Institute contributed data.",
			})[0]
			require.Equal(t, "accepted", result["status"])
			id := result["entry_id"].(string)
			require.NoError(
				t,
				f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
			)
			response := personalRequest(t, r, http.MethodGet, "/api/v1/entries/"+id, "", f.key, true)
			require.Equal(t, 200, response.Code, response.Body.String())
			var read struct{ Data types.Entry }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &read))
			require.Equal(t, "中文资料标题", read.Data.Title)
			require.Equal(t, "这是一句话概述。", read.Data.Description)
			require.Equal(t, types.SummaryStatusCompleted, read.Data.SummaryStatus)
			require.Len(t, read.Data.Domains, 1)
			require.Equal(t, domains.Data[0].ID, read.Data.Domains[0].ID)
			require.Len(t, read.Data.Entities, 1)
			require.Equal(t, entity.Data.ID, read.Data.Entities[0].ID)
			require.NotContains(t, response.Body.String(), `"points"`)
			require.NotContains(t, response.Body.String(), `"key_terms"`)
			again := ingestRevisionHTTP(t, f, map[string]any{
				"library": library, "source_ref": "stable-" + library,
				"title": "English source title", "content": "Understanding Inc recorded original source measurements. Unlisted Research Institute contributed data.",
			})[0]
			require.Equal(t, id, again["entry_id"])
			require.Equal(t, "exists", again["status"])
		})
	}
	var candidates struct{ Data types.EntityCandidatePage }
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entity-candidates", ""), &candidates))
	require.Len(t, candidates.Data.Items, 1)
	require.Equal(t, "Unlisted Research Institute", candidates.Data.Items[0].Name)
	require.EqualValues(t, 4, candidates.Data.Items[0].OccurrenceCount, "all four library types recommend unmatched entities")
}

func TestEntryUnderstandingPreservesManualFieldsAcrossReprocessingAndRevision(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	var domains struct{ Data []types.Domain }
	options := personalRequest(t, f.router, http.MethodGet, "/api/v1/domains", "", f.key, true)
	require.Equal(t, 200, options.Code)
	require.NoError(t, json.Unmarshal(options.Body.Bytes(), &domains))
	var entity struct{ Data types.Entity }
	options = personalRequest(t, f.router, http.MethodPost, "/api/v1/entities",
		`{"canonical_name":"Manual choice fixture","entity_type":"organization","aliases":[]}`, f.key, true)
	require.Equal(t, 201, options.Code)
	require.NoError(t, json.Unmarshal(options.Body.Bytes(), &entity))
	r := ingestAuthHTTPRouter(f)
	call := func(method, path, body string) []byte {
		t.Helper()
		response := personalRequest(t, r, method, path, body, f.key, true)
		require.Equal(t, 200, response.Code, response.Body.String())
		return response.Body.Bytes()
	}
	read := func(id string) types.Entry {
		t.Helper()
		var result struct{ Data types.Entry }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, ""), &result))
		return result.Data
	}
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library":         "personal",
		"source_id":       "manual-understanding",
		"source_revision": 1,
		"content":         "Initial original measurements.",
	})[0]
	id := result["entry_id"].(string)
	model.respond = func(_ context.Context, _ []chat.Message) string {
		call(http.MethodPut, "/api/v1/entries/"+id, `{"title":"人工标题","description":"人工概述。"}`)
		response := personalRequest(
			t,
			f.router,
			http.MethodPut,
			"/api/v1/entries/"+id+"/entities",
			`{"entity_ids":[]}`,
			f.key,
			true,
		)
		require.Equal(t, 200, response.Code, response.Body.String())
		response = personalRequest(
			t,
			f.router,
			http.MethodPut,
			"/api/v1/entries/"+id+"/domains",
			`{"domain_ids":[]}`,
			f.key,
			true,
		)
		require.Equal(t, 200, response.Code, response.Body.String())
		return fmt.Sprintf(`{"title":"模型标题","summary":{"overview":"模型概述。"},
"domain_ids":[%q],"entity_ids":[%q]}`, domains.Data[0].ID, entity.Data.ID)
	}
	process := func() {
		t.Helper()
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
	}
	process()
	model.respond = func(_ context.Context, _ []chat.Message) string {
		return `{"title":"再次生成的标题","summary":{"overview":"再次生成的概述。"},"domain_ids":[],"entity_ids":[]}`
	}
	verify := func() {
		t.Helper()
		entry := read(id)
		require.Equal(t, "人工标题", entry.Title)
		require.Equal(t, "人工概述。", entry.Description)
		require.Equal(t, "人工概述。", entry.StructuredSummary.Overview)
		require.True(t, entry.DomainsManual)
		require.True(t, entry.EntitiesManual)
		require.Empty(t, entry.Domains)
		require.Empty(t, entry.Entities)
	}
	verify()
	call(http.MethodPost, "/api/v1/entries/"+id+"/reprocess", `{}`)
	process()
	verify()
	revised := ingestRevisionHTTP(t, f, map[string]any{
		"library":         "personal",
		"source_id":       "manual-understanding",
		"source_revision": 2,
		"title":           "Replacement source title",
		"content":         "Corrected measurements.",
	})[0]
	require.Equal(t, id, revised["entry_id"])
	process()
	verify()
}

func TestEntryUnderstandingDomainReclassificationRunsInBackground(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	r := ingestAuthHTTPRouter(f)
	call := func(method, path, body string) []byte {
		t.Helper()
		response := personalRequest(t, r, method, path, body, f.key, true)
		require.Equal(t, 200, response.Code, response.Body.String())
		return response.Body.Bytes()
	}
	model.respond = func(_ context.Context, _ []chat.Message) string {
		return `{"title":"已有标题","summary":{"overview":"已有概述。"},"domain_ids":[],"entity_ids":[]}`
	}
	ids := []string{}
	for _, lib := range []string{"official", "references", "personal", "ai-output"} {
		result := ingestRevisionHTTP(t, f, map[string]any{
			"library": lib, "content": "Original measurements for " + lib,
		})[0]
		ids = append(ids, result["entry_id"].(string))
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
	}
	response := personalRequest(
		t,
		f.router,
		http.MethodPut,
		"/api/v1/entries/"+ids[0]+"/domains",
		`{"domain_ids":[]}`,
		f.key,
		true,
	)
	require.Equal(t, 200, response.Code, response.Body.String())
	var created struct{ Data types.Domain }
	response = personalRequest(
		t,
		f.router,
		http.MethodPost,
		"/api/v1/domains",
		`{"name":"测量","definition":"原始测量结果。"}`,
		f.key,
		true,
	)
	require.Equal(t, 201, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	var preview struct{ Data types.EntryBatchPreview }
	request := types.EntryBatchRequest{
		Action:    "reclassify_domains",
		Selection: types.EntryBatchSelection{Filters: &types.EntryBatchFilters{}},
	}
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(call(http.MethodPost, "/api/v1/entries/batches/preview", string(raw)), &preview))
	require.Equal(t, 3, preview.Data.Eligible)
	require.Equal(t, 1, preview.Data.Skipped)
	require.Equal(t, "domains_manual", preview.Data.Reasons[0].Code)
	f.queue.client = taskqueue.NewClient(f.db)
	t.Cleanup(func() { require.NoError(t, f.queue.client.Close()) })
	request.PreviewFingerprint = preview.Data.Fingerprint
	raw, err = json.Marshal(request)
	require.NoError(t, err)
	var batch struct{ Data types.EntryBatchResult }
	require.NoError(t, json.Unmarshal(call(http.MethodPost, "/api/v1/entries/batches", string(raw)), &batch))
	require.Equal(t, "pending", batch.Data.Status)
	// This manual correction arrives after the fixed batch membership was accepted.
	response = personalRequest(
		t,
		f.router,
		http.MethodPut,
		"/api/v1/entries/"+ids[1]+"/domains",
		`{"domain_ids":[]}`,
		f.key,
		true,
	)
	require.Equal(t, 200, response.Code, response.Body.String())
	model.respond = func(_ context.Context, messages []chat.Message) string {
		if strings.Contains(messages[1].Content, "Original measurements for ai-output") {
			response := personalRequest(
				t,
				f.router,
				http.MethodPut,
				"/api/v1/entries/"+ids[3]+"/domains",
				`{"domain_ids":[]}`,
				f.key,
				true,
			)
			require.Equal(t, 200, response.Code, response.Body.String())
		}
		require.Contains(t, messages[1].Content, created.Data.ID)
		return fmt.Sprintf(
			`{"title":"不应替换标题","summary":{"overview":"不应替换概述。"},"domain_ids":[%q],"entity_ids":[]}`,
			created.Data.ID,
		)
	}
	// Use the persisted queue payload to run the accepted background operation.
	var job taskqueue.Job
	require.NoError(t, f.db.Where("id=?", batch.Data.BatchID).First(&job).Error)
	require.NoError(
		t,
		f.articles.(service.EntryBatchService).ProcessEntryBatch(
			f.ctx,
			taskqueue.NewTask(types.TypeEntryBatch, job.Payload),
		),
	)
	require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/batches/"+batch.Data.BatchID, ""), &batch))
	require.Equal(t, "completed", batch.Data.Status)
	require.Equal(t, 1, batch.Data.Succeeded)
	require.Equal(t, 3, batch.Data.Skipped)
	for i, id := range ids {
		var read struct{ Data types.Entry }
		require.NoError(t, json.Unmarshal(call(http.MethodGet, "/api/v1/entries/"+id, ""), &read))
		require.Equal(t, "已有标题", read.Data.Title)
		require.Equal(t, "已有概述。", read.Data.Description)
		if i != 2 {
			require.Empty(t, read.Data.Domains)
		} else {
			require.Len(t, read.Data.Domains, 1)
			require.Equal(t, created.Data.ID, read.Data.Domains[0].ID)
		}
	}
}

func TestEntryUnderstandingLongMaterialMergesAllSegments(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model, maxBodyChars: 400})
	f.seedProcessingModels(t)
	var domains struct{ Data []types.Domain }
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/domains", "", f.key, true)
	require.Equal(t, 200, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &domains))
	model.respond = func(ctx context.Context, messages []chat.Message) string {
		purpose, _ := types.LLMCallMetadataFromContext(ctx)
		if purpose == "article_summary_merge" {
			// All segment observations must be available to the whole-material decision.
			require.Contains(t, messages[1].Content, "第一段的决定")
			require.Contains(t, messages[1].Content, "末段的决定")
			return fmt.Sprintf(`{"title":"合并后的中文标题",
"summary":{"overview":"第一段和末段共同说明完整结果。"},
"domain_ids":[%q,
"missing"],
"entity_ids":[]}`, domains.Data[0].ID)
		}
		overview := "中段的决定。"
		if strings.Contains(messages[1].Content, "STARTMARKER") {
			overview = "第一段的决定。"
		}
		if strings.Contains(messages[1].Content, "ENDMARKER") {
			overview = "末段的决定。"
		}
		return fmt.Sprintf(
			`{"title":"分段标题","summary":{"overview":%q},"domain_ids":[%q],"entity_ids":[]}`,
			overview,
			domains.Data[0].ID,
		)
	}
	body := "STARTMARKER\n\n" + strings.Repeat(
		"A complete paragraph with original measurements and conditions.\n\n",
		20,
	) + "ENDMARKER\n"
	result := ingestRevisionHTTP(t, f, map[string]any{"library": "references", "content": body})[0]
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	r := ingestAuthHTTPRouter(f)
	response = personalRequest(t, r, http.MethodGet, "/api/v1/entries/"+result["entry_id"].(string), "", f.key, true)
	require.Equal(t, 200, response.Code, response.Body.String())
	var read struct{ Data types.Entry }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &read))
	require.Equal(t, "合并后的中文标题", read.Data.Title)
	require.Equal(t, "第一段和末段共同说明完整结果。", read.Data.Description)
	require.Len(t, read.Data.Domains, 1)
}

func TestEntryUnderstandingCallsAreVisibleInAccounting(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		output := `{"title":"统计测试","summary":{"overview":"记录资料理解调用。"},"domain_ids":[],"entity_ids":[]}`
		response := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": output}, "finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 8},
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()
	var db *gorm.DB
	m := &types.Model{
		ID:         "mcp-chat",
		Name:       "fixture",
		SpaceID:    types.SingleSpaceID,
		Type:       types.ModelTypeGeneration,
		Source:     types.ModelSourceRemote,
		Parameters: types.ModelParameters{BaseURL: server.URL + "/v1", Provider: "openai"},
	}
	cfg := chat.ConfigFromModel(m)
	cfg.Accounting = &types.ModelCallAccounting{
		Model: m,
		Write: func(ctx context.Context, call *types.ModelCall) error {
			return modelcalls.NewStore(db).Enqueue(ctx, call)
		},
	}
	model, err := chat.NewChat(cfg, nil)
	require.NoError(t, err)
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	db = f.db
	f.seedProcessingModels(t)
	result := ingestRevisionHTTP(t, f, map[string]any{
		"library": "personal",
		"content": "Original measurements for the accounting fixture.",
	})[0]
	id := result["entry_id"].(string)
	require.NoError(
		t,
		f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
	)
	r := ingestAuthHTTPRouter(f)
	r.GET("/api/v1/entries/:id/model-calls", handler.NewModelHandlerWithMaintenance(nil, f.db).ListModelCalls)
	response := personalRequest(
		t,
		r,
		http.MethodGet,
		"/api/v1/entries/"+id+"/model-calls?purpose=entry_understanding",
		"",
		f.key,
		true,
	)
	require.Equal(t, 200, response.Code, response.Body.String())
	var calls struct{ Data []types.ModelCall }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &calls))
	require.Len(t, calls.Data, 1)
	require.Equal(t, types.CallPurposeEntryUnderstanding, calls.Data[0].Purpose)
	require.Equal(t, id, *calls.Data[0].EntryID)
	require.EqualValues(t, 12, *calls.Data[0].InputTokens)
	require.EqualValues(t, 8, *calls.Data[0].OutputTokens)
	require.True(t, calls.Data[0].Success)
}

func TestEntryUnderstandingCapsValidatedChoicesAndSupportsNoMatches(t *testing.T) {
	model := &understandingChat{}
	f := newMCPFixture(t, mcpProcessingFixture{chat: model})
	f.seedProcessingModels(t)
	entities := []string{}
	domains := []string{}
	for i := 0; i < 4; i++ {
		response := personalRequest(
			t,
			f.router,
			http.MethodPost,
			"/api/v1/entities",
			fmt.Sprintf(`{"canonical_name":"Choice %d","entity_type":"organization","aliases":[]}`, i),
			f.key,
			true,
		)
		require.Equal(t, 201, response.Code, response.Body.String())
		var entity struct{ Data types.Entity }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &entity))
		entities = append(entities, entity.Data.ID)
		response = personalRequest(
			t,
			f.router,
			http.MethodPost,
			"/api/v1/domains",
			fmt.Sprintf(`{"name":"Choice %d","definition":"Original measurements."}`, i),
			f.key,
			true,
		)
		require.Equal(t, 201, response.Code, response.Body.String())
		var domain struct{ Data types.Domain }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &domain))
		domains = append(domains, domain.Data.ID)
	}
	model.respond = func(_ context.Context, _ []chat.Message) string {
		mentions := []map[string]string{}
		for i, id := range entities {
			mentions = append(mentions, map[string]string{"name": fmt.Sprintf("Choice %d", i), "type": "organization", "entity_id": id, "quote": fmt.Sprintf("Choice %d", i)})
		}
		data, err := json.Marshal(
			map[string]any{
				"title":           "选择测试",
				"summary":         map[string]string{"overview": "相关选择。"},
				"domain_ids":      append([]string{"missing"}, domains...),
				"entity_ids":      append([]string{"missing"}, entities...),
				"entity_mentions": mentions,
			},
		)
		require.NoError(t, err)
		return string(data)
	}
	r := ingestAuthHTTPRouter(f)
	for _, noMatches := range []bool{false, true} {
		if noMatches {
			model.respond = func(_ context.Context, _ []chat.Message) string {
				return `{"title":"无匹配测试",
"summary":{"overview":"没有匹配项。"},
"domain_ids":[null,
{},
false,
"missing"],
"entity_ids":"invalid"}`
			}
		}
		result := ingestRevisionHTTP(t, f, map[string]any{
			"library": "references",
			"content": fmt.Sprintf("Choice 0, Choice 1, Choice 2, Choice 3. Original measurements, no matches: %t.", noMatches),
		})[0]
		require.NoError(
			t,
			f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]),
		)
		response := personalRequest(
			t,
			r,
			http.MethodGet,
			"/api/v1/entries/"+result["entry_id"].(string),
			"",
			f.key,
			true,
		)
		require.Equal(t, 200, response.Code, response.Body.String())
		var read struct{ Data types.Entry }
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &read))
		if noMatches {
			require.Empty(t, read.Data.Domains)
			require.Empty(t, read.Data.Entities)
		} else {
			gotDomains, gotEntities := []string{}, []string{}
			for _, item := range read.Data.Domains {
				gotDomains = append(gotDomains, item.ID)
			}
			for _, item := range read.Data.Entities {
				gotEntities = append(gotEntities, item.ID)
			}
			require.ElementsMatch(t, domains[:3], gotDomains)
			require.ElementsMatch(t, entities[:3], gotEntities)
		}
	}
}
