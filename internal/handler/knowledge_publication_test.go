package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestWorkbenchEvidenceHTTPPublicationExclusionCountBeforePagination(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(service.BuiltinAIKnowledgeDomain()).Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Test team"}).Error)
	kb := types.KnowledgeBase{ID: "publication-kb", TenantID: 1, Name: "Publications", DomainID: service.BuiltinAIKnowledgeDomainID, Type: "document"}
	kb.EnsureDefaults()
	require.NoError(t, db.Create(&kb).Error)
	for _, id := range []string{"one", "two"} {
		message := types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: kb.ID, Title: id, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, message.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
		require.NoError(t, db.Create(&message).Error)
		evidence := types.KnowledgeEvidence{ID: "e-" + id, TenantID: 1, KnowledgeBaseID: kb.ID, KnowledgeID: id, Version: 1, BodyHash: id, Body: "Source", PublishedAtMissing: true, Status: types.KnowledgeEvidenceCurrent, CreatedAt: time.Now()}
		require.NoError(t, db.Create(&evidence).Error)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1)))
		c.Next()
	})
	h := NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil)
	router.GET("/knowledge-bases/:id/context/evidence", h.Evidence)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/knowledge-bases/"+kb.ID+"/context/evidence?limit=1", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Excluded int                       `json:"excluded_missing_published_at_count"`
		Data     []types.KnowledgeEvidence `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 2, response.Excluded)
	require.Len(t, response.Data, 1)
	require.True(t, response.Data[0].PublishedAtMissing)
}
