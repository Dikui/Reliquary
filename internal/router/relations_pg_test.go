package router

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"reliquary/internal/application/graph"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"testing"
)

func TestIKB12RelationHTTPGroupingSourcesScopeDeleteAndTypeMerge(t *testing.T) {
	db := entityFixtureDB(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Graph"}).Error)
	require.NoError(t, db.Create(&types.Library{ID: "graph-lib", SpaceID: 1, Name: "Graph library", Directory: "graph-lib", Template: types.TemplateOwn}).Error)
	require.NoError(t, db.Create(&types.LibraryContentType{LibraryID: "graph-lib", Name: "on", Enabled: true, ExtractGraph: true}).Error)
	for _, kind := range []string{"合作", "投资"} {
		require.NoError(t, db.Create(&types.LibraryRelationType{LibraryID: "graph-lib", Name: kind, Enabled: true}).Error)
	}
	for _, id := range []string{"a", "b"} {
		require.NoError(t, db.Create(&types.Entity{ID: id, SpaceID: 1, CanonicalName: id, NormalizedName: id, EntityType: "company"}).Error)
	}
	entry := types.Entry{ID: "graph-entry", SpaceID: 1, LibraryID: "graph-lib", Title: "Evidence source", ContentType: "on", ParseStatus: types.ParseStatusCompleted, Type: types.EntryTypeManual}
	require.NoError(t, db.Create(&entry).Error)
	out := types.EntryProcessingOutput{EntryID: entry.ID, SpaceID: 1, LibraryID: entry.LibraryID, RunID: "run", BodyHash: "hash", Relations: []types.ProcessingRelation{{SubjectID: "a", Type: "合作", ObjectID: "b", Evidence: []types.RelationEvidence{{Locations: []types.SourceLocation{{Start: 3, End: 8}}, Speaker: &types.ProcessingSpeaker{EntityID: "a", Name: "A", Selection: "extracted_person"}}}}}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, 1); err != nil {
			return err
		}
		return graph.ReplaceTx(tx, &entry, &out)
	}))
	engine := gin.New()
	engine.Use(middleware.ErrorHandler(), entityIdentityMiddleware())
	RegisterEntityRoutes(engine.Group("/api/v1"), handler.NewEntityHandler(service.NewEntityService(db, nil)))
	response := entityRequest(t, engine, http.MethodGet, "/api/v1/entities/a/relations?library_id=graph-lib", "", 1, entityOwnerID)
	require.Equal(t, 200, response.Code, response.Body.String())
	var envelope entityEnvelope
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	var groups []service.RelationGroup
	require.NoError(t, json.Unmarshal(envelope.Data, &groups))
	require.Len(t, groups, 1)
	item := groups[0].Relations[0]
	require.Equal(t, "b", item.Peer.ID)
	require.Equal(t, "outgoing", item.Direction)
	require.Equal(t, "Graph library", item.LibraryName)
	require.Equal(t, 1, item.SourceCount)
	require.Equal(t, "Evidence source", item.Sources[0].Title)
	require.Equal(t, 3, item.Sources[0].Evidence[0].Locations[0].Start)
	require.Equal(t, "a", item.Sources[0].Evidence[0].Speaker.EntityID)
	response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities/b/relations", "", 1, entityOwnerID)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"direction":"incoming"`)
	for _, path := range []string{"/api/v1/entities/a/relations?library_id=unknown", "/api/v1/entities/unknown/relations"} {
		require.Equal(t, 404, entityRequest(t, engine, http.MethodGet, path, "", 1, entityOwnerID).Code)
	}
	require.Equal(t, 404, entityRequest(t, engine, http.MethodGet, "/api/v1/entities/a/relations", "", 2, entityOwnerID).Code)
	require.Equal(t, 404, entityRequest(t, engine, http.MethodDelete, "/api/v1/relations/"+item.ID, "", 2, entityOwnerID).Code)
	require.Equal(t, 400, entityRequest(t, engine, http.MethodPost, "/api/v1/libraries/graph-lib/relation-types/merge", `{"from_type":"missing","to_type":"投资"}`, 1, entityOwnerID).Code)
	response = entityRequest(t, engine, http.MethodPost, "/api/v1/libraries/graph-lib/relation-types/merge", `{"from_type":"合作","to_type":"投资"}`, 1, entityOwnerID)
	require.Equal(t, 200, response.Code, response.Body.String())
	response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities/a/relations", "", 1, entityOwnerID)
	require.Contains(t, response.Body.String(), `"relation_type":"投资"`)
	response = entityRequest(t, engine, http.MethodDelete, "/api/v1/relations/"+item.ID, "", 1, entityOwnerID)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := graph.Lock(tx, 1); err != nil {
			return err
		}
		return graph.ReplaceTx(tx, &entry, &out)
	}))
	response = entityRequest(t, engine, http.MethodGet, "/api/v1/entities/a/relations", "", 1, entityOwnerID)
	require.Equal(t, 200, response.Code)
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.JSONEq(t, "[]", string(envelope.Data))
}
