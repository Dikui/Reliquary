package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestKnowledgeContextEntityDetailReturnsEmptyEventArray(t *testing.T) {
	db := fusionFixtureDB(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Team"}).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Name: "Sources", DomainID: service.BuiltinAIKnowledgeDomainID, Type: "document"}
	kb.EnsureDefaults()
	require.NoError(t, db.Create(kb).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "object", TenantID: 1, CanonicalName: "Aurora", NormalizedName: "aurora", EntityType: "model"}).Error)
	require.NoError(t, db.Create(&types.Knowledge{ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Type: types.KnowledgeTypeManual, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: "evidence", TenantID: 1, KnowledgeBaseID: "kb", KnowledgeID: "source", Version: 1, BodyHash: "hash", Body: "Aurora uses Apache-2.0.", Status: types.KnowledgeEvidenceCurrent}).Error)
	require.NoError(t, db.Create(&types.KnowledgeFact{ID: "fact", TenantID: 1, KnowledgeBaseID: "kb", SubjectEntityID: "object", Kind: "attribute", Predicate: "license", Value: types.JSON(`"Apache-2.0"`), ValueType: "text", EvidenceID: "evidence", Quote: "Aurora uses Apache-2.0.", Status: types.KnowledgeFactCurrent}).Error)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	guards := newRBACGuards(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	RegisterKnowledgeContextRoutes(engine.Group("/api/v1"), handler.NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil), guards)
	rec := fusionRequest(t, engine, http.MethodGet, "/api/v1/knowledge-context/entities/object", "", 1, "editor", types.TenantRoleContributor)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.JSONEq(t, "[]", string(response.Data["events"]))
	require.JSONEq(t, "[]", string(response.Data["attention"]))
	var facts []types.KnowledgeFact
	require.NoError(t, json.Unmarshal(response.Data["facts"], &facts))
	require.Len(t, facts, 1)
	require.Equal(t, "license", facts[0].Predicate)
}
