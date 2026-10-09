package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestKnowledgeContextTimelineCoverageResponse(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Timeline test"}).Error)
	domain := service.BuiltinAIKnowledgeDomain()
	domain.ID, domain.TenantID = "timeline-domain", 1
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "timeline-kb", TenantID: 1, DomainID: domain.ID, Name: "Timeline"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "timeline-object", TenantID: 1, CanonicalName: "X", NormalizedName: "x", EntityType: "company"}).Error)
	k := types.Knowledge{ID: "timeline-source", TenantID: 1, KnowledgeBaseID: "timeline-kb", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
	require.NoError(t, db.Create(&k).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "timeline-evidence", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: "X launched.", Status: types.KnowledgeEvidenceCurrent, ExtractionMode: types.KnowledgeExtractionTemplateOnly, ExtractionStatus: types.KnowledgeExtractionComplete}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: "timeline-evidence", EntityID: "timeline-object"}).Error)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	handler := NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil)
	router.POST("/knowledge-context/query", handler.Query)
	req := httptest.NewRequest(http.MethodPost, "/knowledge-context/query", strings.NewReader(`{"scope":"all","entity_id":"timeline-object","include_history":true,"limit":1,"offset":100}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), types.TenantIDContextKey, uint64(1)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Data types.KnowledgeContextResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 1, response.Data.TemplateOnlyMessageCount)
	require.Equal(t, "只包含收录时命中关注范围的消息", response.Data.EventsCoverageNotice)
}
