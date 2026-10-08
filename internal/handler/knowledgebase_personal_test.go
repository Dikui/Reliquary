package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type personalKBResponses struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s *personalKBResponses) CreateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	kb.ID = "personal-kb"
	kb.TenantID = types.SingleTenantID
	s.kb = kb
	return kb, nil
}
func (s *personalKBResponses) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}
func (s *personalKBResponses) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}
func (s *personalKBResponses) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{s.kb}, nil
}

func TestPersonalKnowledgeBaseResponsesKeepEmbeddingWithoutVectorBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &personalKBResponses{}
	h := &KnowledgeBaseHandler{service: service}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), types.SingleTenantID)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.SingleTenantID))
	})
	router.POST("/knowledge-bases", h.CreateKnowledgeBase)
	router.GET("/knowledge-bases", h.ListKnowledgeBases)
	router.GET("/knowledge-bases/:id", h.GetKnowledgeBase)
	routes := []struct {
		method, path, body string
		status             int
	}{
		{http.MethodPost, "/knowledge-bases", `{"domain_id":"domain","name":"Personal","embedding_model_id":"chosen-embedding"}`, http.StatusCreated},
		{http.MethodGet, "/knowledge-bases/personal-kb", "", http.StatusOK},
		{http.MethodGet, "/knowledge-bases", "", http.StatusOK},
	}
	for _, route := range routes {
		request := httptest.NewRequest(route.method, route.path, bytes.NewBufferString(route.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, route.status, response.Code, response.Body.String())
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
		rows := []any{decoded["data"]}
		if route.path == "/knowledge-bases" && route.method == http.MethodGet {
			rows = decoded["data"].([]any)
		}
		require.NotEmpty(t, rows)
		for _, row := range rows {
			fields := row.(map[string]any)
			require.Equal(t, "chosen-embedding", fields["embedding_model_id"])
			for field := range fields {
				require.False(t, strings.HasPrefix(field, "vector_store"), "removed field %s", field)
			}
		}
	}
}
