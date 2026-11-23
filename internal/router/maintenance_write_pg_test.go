package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"reliquary/internal/chatgpt"
	"reliquary/internal/handler"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

const restorePauseReason = "restore awaiting configuration"
const restorePauseMessage = "资料库处于维护暂停（原因：" + restorePauseReason + "），暂不能修改数据；完成恢复/维护后重试"

func maintenanceShelfProposal(libraryID string) map[string]any {
	return map[string]any{
		"library_id": libraryID,
		"source":     map[string]string{"type": "agent", "agent_name": "Synthetic agent"},
		"operations": []map[string]any{{
			"id": "line", "type": "create_entity", "entity": map[string]string{"name": "Maintenance test line", "kind": "product_line"},
		}},
	}
}

func TestMaintenancePauseBlocksBusinessHTTPWritesUntilResume(t *testing.T) {
	f, library := newProductShelfFixture(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f.params.MaintenanceHandler = handler.NewMaintenanceHandler(f.db)
	f.router = NewRouter(f.params)
	path := "/libraries/" + library.ID
	changesets := path + "/shelf/changesets"
	input := maintenanceShelfProposal(library.ID)
	delete(input, "library_id")
	var draft types.ShelfChangeset
	f.request(http.MethodPost, changesets, input, http.StatusCreated, &draft)
	var entity types.Entity
	f.request(http.MethodPost, path+"/entities", map[string]any{"name": "Existing line", "kind": "product_line"}, http.StatusCreated, &entity)
	update := map[string]any{"revision": entity.Revision, "description": "Edited after resume"}
	review := map[string]any{"revision": draft.Revision, "decisions": []map[string]string{{"item_id": "line", "action": "apply"}}}
	confirm := changesets + "/" + draft.ID + "/confirm"
	require.NoError(t, maintenanceops.Pause(t.Context(), f.db, "snapshot", restorePauseReason, nil))

	for _, write := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, changesets, input},
		{http.MethodPatch, changesets + "/" + draft.ID, review},
		{http.MethodPost, confirm, review},
		{http.MethodPatch, "/entities/" + entity.ID, update},
		{http.MethodPost, path + "/shelf/imports", nil},
		{http.MethodPatch, path, map[string]any{"description": "Blocked"}},
	} {
		body := f.request(write.method, write.path, write.body, http.StatusServiceUnavailable, nil)
		require.Contains(t, string(body), restorePauseMessage)
	}
	shelfUpload(t, f, library.ID, "synthetic attachment", http.StatusServiceUnavailable)
	var saved types.ShelfChangeset
	f.request(http.MethodGet, changesets+"/"+draft.ID, nil, http.StatusOK, &saved)
	require.Equal(t, draft.Revision, saved.Revision)
	require.Equal(t, "pending", saved.Status)

	require.NoError(t, maintenanceops.Resume(t.Context(), f.db))
	f.request(http.MethodPost, confirm, review, http.StatusOK, &saved)
	require.Equal(t, "confirmed", saved.Status)
	f.request(http.MethodPatch, "/entities/"+entity.ID, update, http.StatusOK, &entity)
	require.Equal(t, "Edited after resume", entity.Description)
	shelfUpload(t, f, library.ID, "synthetic attachment", http.StatusCreated)
}

func TestMaintenancePauseAllowsRecoveryConfigurationAndReadRoutes(t *testing.T) {
	f := newPersonalAuthFixture(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("CHATGPT_CALLBACK_URL", "http://127.0.0.1:18117/auth/callback")
	f.params.MaintenanceHandler = handler.NewMaintenanceHandler(f.db)
	f.params.ChatGPTHandler = handler.NewChatGPTHandler(chatgpt.NewManager(f.db))
	f.router = NewRouter(f.params)
	require.NoError(t, maintenanceops.Pause(t.Context(), f.db, "snapshot", restorePauseReason, nil))

	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/v1/libraries", "", http.StatusOK},
		{http.MethodGet, "/api/v1/maintenance/status", "", http.StatusOK},
		{http.MethodPost, "/api/v1/models", `{"name":"Recovery model","type":"KnowledgeQA","source":"remote","parameters":{}}`, http.StatusCreated},
		{http.MethodPost, "/api/v1/chatgpt/login", `{}`, http.StatusOK},
		{http.MethodGet, "/auth/callback?state=invalid&code=synthetic", "", http.StatusBadRequest},
		{http.MethodPost, "/api/v1/space/api-keys", `{"name":"Recovery key"}`, http.StatusCreated},
		{http.MethodPut, "/api/v1/space", `{"name":"Recovery space","description":"Personal library"}`, http.StatusOK},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			response := personalRequest(t, f.router, test.method, test.path, test.body, "", false)
			require.Equal(t, test.status, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), restorePauseMessage)
		})
	}
}

func TestMaintenancePauseLibraryRoutesAllowOnlyModelTierChanges(t *testing.T) {
	f, library := newProductShelfFixture(t)
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f.params.MaintenanceHandler = handler.NewMaintenanceHandler(f.db)
	f.router = NewRouter(f.params)
	for _, model := range []types.Model{
		// newMCPFixture binds the default library's light and generation tiers to this model.
		{ID: "mcp-chat", Type: types.ModelTypeGeneration},
		{ID: "recovery-generation", Type: types.ModelTypeGeneration},
		{ID: "recovery-rerank", Type: types.ModelTypeRerank},
		{ID: "recovery-embedding", Type: types.ModelTypeEmbedding},
		{ID: "legacy-embedding", Type: types.ModelTypeEmbedding},
	} {
		model.Name, model.SpaceID, model.Source, model.Status = model.ID, types.SingleSpaceID, types.ModelSourceRemote, types.ModelStatusActive
		require.NoError(t, f.db.Create(&model).Error)
	}
	path := "/libraries/" + library.ID
	require.NoError(t, maintenanceops.Pause(t.Context(), f.db, "snapshot", restorePauseReason, nil))
	var saved types.Library
	f.request(http.MethodPatch, path, map[string]any{"model_tiers": types.ModelTierConfig{
		Generation: "recovery-generation", Light: "recovery-generation", EntityResolution: "recovery-generation", Rerank: "recovery-rerank",
	}}, http.StatusOK, &saved)
	require.Equal(t, "recovery-generation", saved.SummaryModelID)
	require.Equal(t, "recovery-generation", saved.ModelTiers.Light)
	require.Equal(t, "recovery-generation", saved.ModelTiers.EntityResolution)
	require.Equal(t, "recovery-rerank", saved.ModelTiers.Rerank)
	for _, payload := range []map[string]any{
		{"shelf_config": map[string]any{}},
		{"entity_schema": map[string]any{}},
		{"model_tiers": types.ModelTierConfig{}, "shelf_config": map[string]any{}},
	} {
		body := f.request(http.MethodPut, path, payload, http.StatusServiceUnavailable, nil)
		require.Contains(t, string(body), restorePauseMessage)
		if _, mixed := payload["model_tiers"]; mixed {
			require.Contains(t, string(body), "请单独提交模型档位")
		}
	}
	var reread types.Library
	f.request(http.MethodGet, path, nil, http.StatusOK, &reread)
	require.Equal(t, saved.ModelTiers, reread.ModelTiers)
	require.Equal(t, library.ShelfConfig, reread.ShelfConfig)
	selected := saved.ModelTiers
	selected.Embedding = "recovery-embedding"
	f.request(http.MethodPut, path, map[string]any{"model_tiers": selected}, http.StatusOK, &saved)
	require.Equal(t, "recovery-embedding", saved.EmbeddingModelID)
	var changesets types.ShelfChangesetPage
	f.request(http.MethodGet, path+"/shelf/changesets", nil, http.StatusOK, &changesets)
	require.Zero(t, changesets.Total)

	var legacy types.Library
	f.request(http.MethodGet, "/library", nil, http.StatusOK, &legacy)
	legacyTiers := legacy.ModelTiers
	legacyTiers.Embedding = "legacy-embedding"
	f.request(http.MethodPut, "/library", map[string]any{"model_tiers": legacyTiers}, http.StatusOK, &saved)
	require.Equal(t, "legacy-embedding", saved.EmbeddingModelID)
	requested := legacyTiers
	requested.Embedding = ""
	body := f.request(http.MethodPut, "/library", map[string]any{
		"name": "Blocked rename", "description": legacy.Description, "model_tiers": requested,
	}, http.StatusServiceUnavailable, nil)
	require.Contains(t, string(body), "请单独提交模型档位")
	f.request(http.MethodGet, "/library", nil, http.StatusOK, &reread)
	require.Equal(t, legacy.Name, reread.Name)
	require.Equal(t, legacyTiers, reread.ModelTiers)
}

func TestMaintenancePauseBlocksMCPWritesAndKeepsReadsAvailable(t *testing.T) {
	f, library := newProductShelfFixture(t)
	client := f.connect(t, "2025-11-25")
	input := maintenanceShelfProposal(library.ID)
	var draft types.ShelfChangeset
	mcpCall(t, client, "shelf_submit", input, &draft)
	require.NoError(t, maintenanceops.Pause(t.Context(), f.db, "snapshot", restorePauseReason, nil))
	for _, call := range []struct {
		name  string
		input any
	}{
		{"shelf_submit", input},
		{"shelf_submit", map[string]any{"library_id": library.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true}},
		{"create_topic", map[string]string{"library_id": library.ID, "name": "Blocked topic"}},
		{"ingest", map[string]any{"items": []map[string]string{{"library": library.ID, "content": "Synthetic material"}}}},
	} {
		out := shelfMCPError(t, client, call.name, call.input)
		require.Equal(t, restorePauseMessage, out.Content[0].(*mcp.TextContent).Text)
	}
	var result types.ShelfFindResult
	mcpCall(t, client, "shelf_find", map[string]any{"library_id": library.ID, "q": "Maintenance test line"}, &result)
	require.Zero(t, result.Total)
	var libraries struct{ Libraries []json.RawMessage }
	mcpCall(t, client, "list_libraries", map[string]any{}, &libraries)
	require.NotEmpty(t, libraries.Libraries)

	require.NoError(t, maintenanceops.Resume(t.Context(), f.db))
	mcpCall(t, client, "shelf_submit", map[string]any{
		"library_id": library.ID, "changeset_id": draft.ID, "revision": draft.Revision, "confirmed": true,
		"decisions": []map[string]string{{"item_id": "line", "action": "apply"}},
	}, &draft)
	require.Equal(t, "confirmed", draft.Status)
}
