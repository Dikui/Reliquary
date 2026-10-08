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
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestIssue06FocusRoutesUseTenantRules(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Focus team"}).Error)
	domain := service.BuiltinAIKnowledgeDomain()
	domain.ID, domain.TenantID = "focus-domain", 1
	require.NoError(t, db.Create(domain).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "focus-kb", TenantID: 1, DomainID: domain.ID, Name: "Sources"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "object", TenantID: 1, CanonicalName: "Aurora", NormalizedName: "aurora", EntityType: "model"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeTag{ID: "topic", TenantID: 1, KnowledgeBaseID: "focus-kb", Kind: types.TagKindTopic, Name: "Models"}).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), fusionIdentityMiddleware())
	guards := newAccessGuards(nil, nil, nil)
	RegisterKnowledgeContextRoutes(engine.Group("/api/v1"), handler.NewKnowledgeContextHandler(service.NewKnowledgeContextService(db, nil), nil), guards)
	for _, target := range []string{"/api/v1/knowledge-context/entities/object/star", "/api/v1/knowledge-context/topics/topic/star"} {
		foreign := fusionRequest(t, engine, http.MethodPut, target, "", 2, "editor")
		require.Equal(t, http.StatusNotFound, foreign.Code, foreign.Body.String())
		added := fusionRequest(t, engine, http.MethodPut, target, "", 1, "editor")
		require.Equal(t, http.StatusOK, added.Code, added.Body.String())
	}
	focus := fusionRequest(t, engine, http.MethodGet, "/api/v1/knowledge-context/focus?scope=team", "", 1, "personal-user")
	require.Equal(t, http.StatusOK, focus.Code, focus.Body.String())
	var response struct {
		Data types.KnowledgeFocusScope `json:"data"`
	}
	require.NoError(t, json.Unmarshal(focus.Body.Bytes(), &response))
	require.Len(t, response.Data.StarredEntities, 1)
	require.Len(t, response.Data.StarredTopics, 1)
	require.Len(t, response.Data.AvailableTopics, 1)
	require.Equal(t, "Sources", response.Data.StarredTopics[0].KnowledgeBaseName)
	for _, target := range []string{"/api/v1/knowledge-context/entities/object/star", "/api/v1/knowledge-context/topics/topic/star"} {
		removed := fusionRequest(t, engine, http.MethodDelete, target, "", 1, "editor")
		require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	}
}
