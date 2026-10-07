package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// stubListKBService returns a fixed slice from ListKnowledgeBases. Only
// the methods exercised by ListKnowledgeBases are implemented; embedding
// the interface keeps the rest nil-panic'ing intentionally.
type stubListKBService struct {
	interfaces.KnowledgeBaseService
	kbs []*types.KnowledgeBase
}

func (s *stubListKBService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return s.kbs, nil
}

// stubVectorStoreService satisfies the two service methods the list
// path depends on: BatchResolveStoreView for bound KBs and
// EnvDefaultStoreView for env-fallback KBs. ResolveStoreView is
// intentionally left nil because ListKnowledgeBases must never reach
// into the single-KB resolver — doing so per row would be the N+1
// pattern this path is designed to avoid.
type stubVectorStoreService struct {
	interfaces.VectorStoreService
	batch      map[string]types.StoreDisplay
	batchCalls int
	batchErr   error
	envView    types.StoreDisplay
}

func (s *stubVectorStoreService) BatchResolveStoreView(
	_ context.Context, _ uint64, storeIDs []string,
) (map[string]types.StoreDisplay, error) {
	s.batchCalls++
	if s.batchErr != nil {
		return nil, s.batchErr
	}
	out := make(map[string]types.StoreDisplay, len(storeIDs))
	for _, id := range storeIDs {
		if v, ok := s.batch[id]; ok {
			out[id] = v
		} else {
			out[id] = types.UnavailableStoreDisplay()
		}
	}
	return out, nil
}

func (s *stubVectorStoreService) EnvDefaultStoreView(_ context.Context) types.StoreDisplay {
	if s.envView.Source == "" {
		return types.DefaultStoreDisplay()
	}
	return s.envView
}

func newListKBRouter(
	t *testing.T,
	svc interfaces.KnowledgeBaseService,
	vss interfaces.VectorStoreService,
) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "u-test")
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc, vectorStoreService: vss}
	r.GET("/knowledge-bases", h.ListKnowledgeBases)
	return r
}

func TestListKB_EnrichesEnvAndBoundStores(t *testing.T) {
	storeUserA := "aaaa-bbbb-cccc-dddd"

	kbs := []*types.KnowledgeBase{
		{ID: "kb-env", Name: "env", TenantID: 1},
		{ID: "kb-bound", Name: "bound", TenantID: 1, VectorStoreID: &storeUserA},
	}
	vss := &stubVectorStoreService{
		batch: map[string]types.StoreDisplay{
			storeUserA: {
				Name:       "prod-postgres",
				Source:     types.StoreSourceUser,
				EngineType: "postgres",
				Status:     "available",
			},
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases", nil)
	newListKBRouter(t, &stubListKBService{kbs: kbs}, vss).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var envelope struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if !envelope.Success || len(envelope.Data) != 2 {
		t.Fatalf("expected 2 rows, got %d body=%s", len(envelope.Data), w.Body.String())
	}

	byID := map[string]map[string]interface{}{}
	for _, row := range envelope.Data {
		byID[row["id"].(string)] = row
	}

	// 1) env-store KB — System default labelling, no engine type.
	envRow := byID["kb-env"]
	if envRow["vector_store_source"] != string(types.StoreSourceEnv) {
		t.Errorf("env KB: expected source=env, got %v", envRow["vector_store_source"])
	}
	if name, _ := envRow["vector_store_name"].(string); name == "" {
		t.Errorf("env KB: expected non-empty system-default name")
	}

	// 2) own-tenant bound KB — name + engine surfaced.
	boundRow := byID["kb-bound"]
	if boundRow["vector_store_source"] != string(types.StoreSourceUser) {
		t.Errorf("bound KB: expected source=user, got %v", boundRow["vector_store_source"])
	}
	if boundRow["vector_store_name"] != "prod-postgres" {
		t.Errorf("bound KB: expected name=prod-postgres, got %v", boundRow["vector_store_name"])
	}
	if boundRow["vector_store_engine_type"] != "postgres" {
		t.Errorf("bound KB: expected engine=postgres, got %v", boundRow["vector_store_engine_type"])
	}

}

func TestListKB_BatchesStoreLookupsToAvoidNPlus1(t *testing.T) {
	// Five KBs bound to three distinct stores. The list endpoint must
	// resolve them in a single BatchResolveStoreView call regardless of
	// row count — calling the per-KB ResolveStoreView path inside the
	// loop would issue one service call per KB (the N+1 pattern this
	// test pins against).
	s1, s2, s3 := "store-1", "store-2", "store-3"
	kbs := []*types.KnowledgeBase{
		{ID: "a", TenantID: 1, VectorStoreID: &s1},
		{ID: "b", TenantID: 1, VectorStoreID: &s2},
		{ID: "c", TenantID: 1, VectorStoreID: &s1}, // dup
		{ID: "d", TenantID: 1, VectorStoreID: &s3},
		{ID: "e", TenantID: 1}, // env, no store call
	}
	vss := &stubVectorStoreService{
		batch: map[string]types.StoreDisplay{
			s1: {Name: "s1", Source: types.StoreSourceUser, EngineType: "postgres", Status: "available"},
			s2: {Name: "s2", Source: types.StoreSourceUser, EngineType: "postgres", Status: "available"},
			s3: {Name: "s3", Source: types.StoreSourceUser, EngineType: "postgres", Status: "available"},
		},
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases", nil)
	newListKBRouter(t, &stubListKBService{kbs: kbs}, vss).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if vss.batchCalls != 1 {
		t.Fatalf("expected exactly 1 batch store-view call (N+1 protection), got %d", vss.batchCalls)
	}
}

func TestListKB_GracefullyDegradesWhenBatchResolveFails(t *testing.T) {
	// If the store-view resolver fails, the list response must still
	// succeed — bound KBs render as unavailable. The list endpoint is
	// not allowed to 500 just because the vector-store service is
	// momentarily unhealthy.
	storeID := "aaaa-bbbb"
	kbs := []*types.KnowledgeBase{
		{ID: "kb", TenantID: 1, VectorStoreID: &storeID},
	}
	vss := &stubVectorStoreService{batchErr: errSentinel("infra glitch")}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases", nil)
	newListKBRouter(t, &stubListKBService{kbs: kbs}, vss).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even when batch resolve fails, got %d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	if len(envelope.Data) != 1 {
		t.Fatalf("expected 1 row, got %d", len(envelope.Data))
	}
	if envelope.Data[0]["vector_store_source"] != string(types.StoreSourceUnavailable) {
		t.Errorf("expected fallback source=unavailable, got %v", envelope.Data[0]["vector_store_source"])
	}
}
