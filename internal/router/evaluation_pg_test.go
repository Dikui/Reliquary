package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type evaluationHTTPBases struct{ interfaces.LibraryService }

func (evaluationHTTPBases) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestEvaluationHTTPAndAllSearchOrigins(t *testing.T) {
	f := newMCPFixture(t)
	// These HTTP and MCP adapters share the production search service contract.
	search := service.NewSearchService(evaluationHTTPBases{f.params.LibraryService}, f.params.SpaceService, nil, f.db)
	f.router.POST("/api/v1/search", handler.NewSearchHandler(search, nil).SearchEntry)
	evaluations := service.NewEvaluationService(f.db, search, nil)
	handler.NewEvaluationHandler(evaluations, service.NewSearchRecorder(f.db)).Register(f.router.Group("/api/v1"))
	f.seedArticle(t, "Evaluation searchfixture", "", "# Question\n\nsearchfixture evidence")
	body, _ := json.Marshal(types.EntrySearchRequest{Query: "searchfixture", Libraries: []string{f.library.ID}})
	for _, credential := range []struct {
		key string
		api bool
	}{{f.token, false}, {f.key, true}} {
		response := personalRequest(t, f.router, http.MethodPost, "/api/v1/search", string(body), credential.key, credential.api)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	client := f.connect(t, "2025-11-25")
	var result map[string]any
	mcpCall(t, client, "search", map[string]any{"query": "searchfixture", "libraries": []string{f.library.ID}}, &result)
	records, err := service.NewSearchRecorder(f.db).List(f.ctx, service.SearchRecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 3)
	origins := []string{}
	for _, r := range records {
		origins = append(origins, r.Source)
	}
	require.ElementsMatch(t, []string{"web", "http", "mcp"}, origins)
	created := personalRequest(t, f.router, "POST", "/api/v1/evaluation/sets", `{"name":"HTTP evaluation","description":"Test"}`, f.key, true)
	require.Equal(t, 200, created.Code, created.Body.String())
	var set struct {
		Data types.EvaluationSet `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &set))
	converted := personalRequest(t, f.router, "POST", "/api/v1/evaluation/sets/"+set.Data.ID+"/from-record", `{"record_id":"`+records[0].ID+`","answers":[]}`, f.key, true)
	require.Equal(t, 200, converted.Code, converted.Body.String())
	var question struct {
		Data types.EvaluationQuestion `json:"data"`
	}
	require.NoError(t, json.Unmarshal(converted.Body.Bytes(), &question))
	get := personalRequest(t, f.router, "GET", "/api/v1/evaluation/sets/"+set.Data.ID+"/questions/"+question.Data.ID, "", f.token, false)
	require.Equal(t, 200, get.Code)
	runResponse := personalRequest(t, f.router, "POST", "/api/v1/evaluation/sets/"+set.Data.ID+"/runs", `{"k":2}`, f.key, true)
	require.Equal(t, 200, runResponse.Code, runResponse.Body.String())
	var run struct {
		Data types.EvaluationRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(runResponse.Body.Bytes(), &run))
	require.Equal(t, "completed", run.Data.Status)
	listed := personalRequest(t, f.router, "GET", "/api/v1/search/records?source=evaluation", "", f.key, true)
	require.Equal(t, 200, listed.Code)
	require.Contains(t, listed.Body.String(), run.Data.ID)
	unauthorized := personalRequest(t, f.router, "GET", "/api/v1/evaluation/sets", "", "invalid-key", true)
	require.Equal(t, 401, unauthorized.Code)
	invalid := personalRequest(t, f.router, "POST", "/api/v1/evaluation/sets/"+set.Data.ID+"/runs", `{"k":-1}`, f.key, true)
	require.Equal(t, 400, invalid.Code)
	missing := personalRequest(t, f.router, "GET", "/api/v1/evaluation/runs/missing", "", f.key, true)
	require.Equal(t, 404, missing.Code)
}
