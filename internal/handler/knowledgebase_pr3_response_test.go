package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// stubKBCreateService drives CreateKnowledgeBase end-to-end with a
// service that returns a chosen error. Embedding the interface keeps
// any other method nil-panic'ing on purpose.
type stubKBCreateService struct {
	interfaces.KnowledgeBaseService
	createErr error
}

func (s *stubKBCreateService) CreateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	kb.ID = "kb-new"
	kb.TenantID = 1
	return kb, nil
}

func newCreateKBRouter(svc interfaces.KnowledgeBaseService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), "u-test")
		c.Next()
	})
	h := &KnowledgeBaseHandler{service: svc}
	r.POST("/knowledge-bases", h.CreateKnowledgeBase)
	return r
}

func TestCreateKB_PreservesTypedErrorCode_2200(t *testing.T) {
	svc := &stubKBCreateService{
		createErr: apperrors.NewVectorStoreBindingInvalidError("vector store not found"),
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases",
		strings.NewReader(`{"name":"kb"}`))
	req.Header.Set("Content-Type", "application/json")
	newCreateKBRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"code":2200`) {
		t.Fatalf("expected envelope to contain code 2200, got %s", body)
	}
	if strings.Contains(body, `"code":1007`) || strings.Contains(body, `"code":1000`) {
		t.Fatalf("typed error must not be wrapped into a generic code, got %s", body)
	}
}

func TestCreateKB_PreservesTypedErrorCode_2201(t *testing.T) {
	svc := &stubKBCreateService{
		createErr: apperrors.NewVectorStoreUnavailableError(""),
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases",
		strings.NewReader(`{"name":"kb"}`))
	req.Header.Set("Content-Type", "application/json")
	newCreateKBRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":2201`) {
		t.Fatalf("expected envelope to contain code 2201, got %s", w.Body.String())
	}
}

func TestCreateKB_GenericErrorStillFallsThroughTo500(t *testing.T) {
	// A non-AppError must NOT be auto-rewritten to 200/400 — operational
	// monitoring still needs to see infrastructure failures as 5xx.
	svc := &stubKBCreateService{createErr: errSentinel("connection refused")}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/knowledge-bases",
		strings.NewReader(`{"name":"kb"}`))
	req.Header.Set("Content-Type", "application/json")
	newCreateKBRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for raw infra error, got %d body=%s", w.Code, w.Body.String())
	}
}

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestBuildKBResponse_KeepsVectorStoreIDForOwnerKB(t *testing.T) {
	// Same setup but with the user-source display — owner caller should
	// still see the UUID alongside the resolved metadata.
	storeID := "aaaa-bbbb-cccc-dddd"
	kb := &types.KnowledgeBase{
		ID:               "kb-1",
		Name:             "owner-kb",
		TenantID:         1,
		EmbeddingModelID: "e",
		SummaryModelID:   "s",
		VectorStoreID:    &storeID,
	}
	view := types.StoreDisplay{
		Name:       "prod-postgres",
		Source:     types.StoreSourceUser,
		EngineType: "postgres",
		Status:     "available",
	}
	got := buildKBResponse(kb, view, nil)
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", got)
	}
	if m["vector_store_id"] != storeID {
		t.Fatalf("owner KB must keep vector_store_id, got %v", m["vector_store_id"])
	}
	if m["vector_store_name"] != "prod-postgres" {
		t.Fatalf("owner KB must surface store name, got %v", m["vector_store_name"])
	}
}
