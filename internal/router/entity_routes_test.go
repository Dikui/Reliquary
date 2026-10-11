package router

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

func TestEntityRoutesFilterValidatePaginationAndReturnArticleDetail(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Entity routes"}).Error)
	kb := types.KnowledgeBase{ID: "route-kb", SpaceID: 1, Name: "Articles", Type: "document"}
	kb.EnsureDefaults()
	require.NoError(t, db.Create(&kb).Error)
	entity := types.Entity{ID: "route-entity", SpaceID: 1, CanonicalName: "Acme", NormalizedName: "acme", EntityType: "company"}
	require.NoError(t, db.Create(&entity).Error)
	require.NoError(t, db.Create(&types.EntityAlias{ID: "route-alias", SpaceID: 1, EntityID: entity.ID, EntityType: "company", Alias: "艾克米", NormalizedAlias: "艾克米", ResolutionMethod: "exact", CreatedAt: time.Now()}).Error)
	article := types.Entry{ID: "route-article", SpaceID: 1, KnowledgeBaseID: kb.ID, Title: "Article mention", Type: types.EntryTypeManual, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
	require.NoError(t, db.Create(&article).Error)
	require.NoError(t, db.Create(&types.EntryEntity{SpaceID: 1, EntryID: article.ID, EntityID: entity.ID, CreatedAt: time.Now()}).Error)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	RegisterEntityRoutes(engine.Group("/api/v1"), handler.NewEntityHandler(service.NewEntityService(db, nil)))
	response := entityRequest(t, engine, http.MethodGet, "/api/v1/entities?entity_type=company&entity=艾克米&limit=1&offset=0", "", 1, entityOwnerID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope entityEnvelope
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	var entities []types.Entity
	require.NoError(t, json.Unmarshal(envelope.Data, &entities))
	require.Len(t, entities, 1)
	require.Equal(t, entity.ID, entities[0].ID)
	response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities/route-entity", "", 1, entityOwnerID)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	var detail types.EntityDetail
	require.NoError(t, json.Unmarshal(envelope.Data, &detail))
	require.Equal(t, "company", detail.Entity.EntityType)
	require.Equal(t, []string{"艾克米"}, detail.Entity.Aliases)
	require.Len(t, detail.Entries, 1)
	require.Equal(t, article.ID, detail.Entries[0].ID)
	for _, query := range []string{"limit=0", "limit=101", "offset=-1"} {
		response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities?"+query, "", 1, entityOwnerID)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities/route-entity", "", 2, entityOwnerID)
	require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
}

func TestEntityResponsesEncodeEmptyAliasesAndArticlesAsArrays(t *testing.T) {
	encoded, err := json.Marshal(types.EntityDetail{Entity: types.Entity{ID: "empty-entity", EntityType: "other"}})
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.JSONEq(t, "[]", string(fields["entries"]))
	var entityFields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(fields["entity"], &entityFields))
	require.JSONEq(t, "[]", string(entityFields["aliases"]))
}
