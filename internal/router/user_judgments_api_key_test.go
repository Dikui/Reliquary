package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

func TestAPIKeyJudgmentsPersistAuditAndRefreshHeader(t *testing.T) {
	f := newPersonalAuthFixture(t)
	root := t.TempDir()
	v, err := service.NewVaultService(f.db, &config.Config{Vault: &config.VaultConfig{Root: root, HeaderDebounce: 20 * time.Millisecond}})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, v.Close()) })
	f.params.KnowledgeService.(interface{ SetVault(*service.VaultService) }).SetVault(v)
	logs := service.NewOperationLogService(f.db)
	topics := service.NewTopicService(f.db)
	topics.SetOperationLogs(logs)
	topics.SetHeaderRefresher(v)
	group := f.router.Group("/api/v1")
	RegisterTopicRoutes(group, handler.NewTopicHandler(topics), newAccessGuards(f.params.KBService, f.params.KnowledgeService, nil))
	group.GET("/operation-logs", handler.NewOperationLogHandler(logs).List)
	kbResponse := personalRequest(t, f.router, http.MethodGet, "/api/v1/knowledge-base", "", f.key, true)
	require.Equal(t, http.StatusOK, kbResponse.Code, kbResponse.Body.String())
	var kb struct{ Data types.KnowledgeBase }
	require.NoError(t, json.Unmarshal(kbResponse.Body.Bytes(), &kb))
	k := &types.Knowledge{ID: uuid.NewString(), TenantID: types.SingleTenantID, KnowledgeBaseID: kb.Data.ID, Title: "API fixture", Type: "manual", ContentType: "event", ProcessingMode: "full", SourceKind: "article", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, f.db.Create(k).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	require.NoError(t, v.WriteArticle(ctx, k, "API original body.\n", nil))
	r := personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge/"+k.ID+"/star", `{"starred":true}`, f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	r = personalRequest(t, f.router, http.MethodPost, "/api/v1/topics", `{"name":"API topic","description":"API fixture observations"}`, f.key, true)
	require.Equal(t, http.StatusCreated, r.Code, r.Body.String())
	var topic struct{ Data types.Topic }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &topic))
	r = personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge/"+k.ID+"/topics", `{"topic_ids":["`+topic.Data.ID+`"]}`, f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	r = personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge/"+k.ID+"/content-type", `{"content_type":"research"}`, f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/operation-logs?page=1&page_size=100", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var result struct{ Data types.OperationLogPage }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
	require.EqualValues(t, 4, result.Data.Total)
	for _, entry := range result.Data.Items {
		require.Contains(t, entry.Actor, "api_key:")
		require.NotEmpty(t, entry.Before)
		require.NotEmpty(t, entry.After)
	}
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(root, k.VaultPath))
		if err != nil {
			return false
		}
		header, body, err := service.ParseVaultArticle(data)
		return err == nil && header.Starred && header.ContentType == "research" && len(header.Topics) == 1 && header.Topics[0] == "API topic" && string(body) == "API original body.\n"
	}, time.Second, 10*time.Millisecond)
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/operation-logs?operation_type=star", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
	require.EqualValues(t, 1, result.Data.Total)
	for _, invalid := range []string{`{"starred":"yes"}`, `{}`} {
		r = personalRequest(t, f.router, http.MethodPut, "/api/v1/knowledge/"+k.ID+"/star", invalid, f.key, true)
		require.Equal(t, http.StatusBadRequest, r.Code)
	}
	r = personalRequest(t, f.router, http.MethodGet, "/api/v1/operation-logs?operation_type=unknown", "", f.key, true)
	require.Equal(t, http.StatusBadRequest, r.Code)
}

func TestOperationLogActorNames(t *testing.T) {
	f := newPersonalAuthFixture(t)
	logs := service.NewOperationLogService(f.db)
	f.router.GET("/api/v1/operation-logs", handler.NewOperationLogHandler(logs).List)
	tenantID := uint64(types.SingleTenantID)
	now := time.Now()
	keys := []types.TenantAPIKey{
		{ID: 101, TenantID: &tenantID, Name: "Collector", KeyHash: "actor-name-101", RevokedAt: &now},
		{ID: 102, TenantID: &tenantID, Name: "", KeyHash: "actor-name-102"},
	}
	require.NoError(t, f.db.Create(&keys).Error)
	for _, actor := range []string{"web_user:1", "api_key:101", "api_key:102", "api_key:103"} {
		require.NoError(t, f.db.Create(&types.OperationLog{ID: uuid.NewString(), TenantID: tenantID, Actor: actor, OperationType: "star", ObjectID: "article-a", Before: types.JSON("false"), After: types.JSON("true"), CreatedAt: now}).Error)
	}
	r := personalRequest(t, f.router, http.MethodGet, "/api/v1/operation-logs?page=1&page_size=100", "", f.key, true)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var result struct{ Data types.OperationLogPage }
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &result))
	require.EqualValues(t, 4, result.Data.Total)
	names := map[string]string{}
	for _, item := range result.Data.Items {
		names[item.Actor] = item.ActorName
	}
	require.Equal(t, map[string]string{"web_user:1": "", "api_key:101": "Collector", "api_key:102": "", "api_key:103": ""}, names)
	require.NotContains(t, r.Body.String(), "key_hash")
	require.NotContains(t, r.Body.String(), `"api_key":`)
}
