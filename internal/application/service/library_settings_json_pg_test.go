package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestLibrarySettingsSavePreservesUneditedStoredJSON(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	require.NoError(t, db.Exec(`INSERT INTO models(id,space_id,name,type,source,status,parameters) VALUES('gen',1,'Generation','KnowledgeQA','remote','active','{}'),('embed',1,'Embedding','Embedding','remote','active','{}'),('new-embed',1,'New embedding','Embedding','remote','active','{}'); INSERT INTO libraries(id,space_id,name,description,embedding_model_id,summary_model_id,chunking_config,model_tiers,indexing_strategy) VALUES('library',1,'Original','Retained','embed','gen','{"chunk_size":512,"chunk_overlap":0,"separators":null,"languages":null,"parser_engine_rules":null,"extension":{"array":[],"null":null}}','{"generation":"gen","embedding":"embed","light":null,"extension":null}','null')`).Error)
	svc := &libraryService{db: db, repo: repository.NewLibraryRepository(db)}
	read := func() map[string]any {
		var value map[string]any
		require.NoError(t, db.Table("libraries").Select("chunking_config::text AS chunking_config, model_tiers::text AS model_tiers, indexing_strategy::text AS indexing_strategy").Where("id='library'").Take(&value).Error)
		return value
	}
	original := read()
	_, err := svc.UpdateLibrary(ctx, "library", "Renamed", "Retained", nil)
	require.NoError(t, err)
	require.Equal(t, original, read())
	// The page changes only the embedding selection, retaining the loaded
	// model-tier object rather than inserting defaults for its other fields.
	tiers := &types.ModelTierConfig{Generation: "gen", Embedding: "new-embed"}
	_, err = svc.UpdateLibrary(ctx, "library", "Renamed", "Retained", &types.LibraryConfig{ModelTiers: tiers})
	require.NoError(t, err)
	updated := read()
	require.Equal(t, original["chunking_config"], updated["chunking_config"])
	require.Equal(t, original["indexing_strategy"], updated["indexing_strategy"])
	require.JSONEq(t, `{"generation":"gen","embedding":"new-embed","light":null,"extension":null}`, updated["model_tiers"].(string))
	// A real chunk-size edit must not persist the separators used only as
	// display defaults by GET /library.
	chunks := &types.ChunkingConfig{ChunkSize: 128, ChunkOverlap: 0, ChunkOverlapSet: true, Separators: []string{"\n\n", "\n", "。"}}
	_, err = svc.UpdateLibrary(ctx, "library", "Renamed", "Retained", &types.LibraryConfig{ChunkingConfig: chunks})
	require.NoError(t, err)
	updated = read()
	require.JSONEq(t, `{"chunk_size":128,"chunk_overlap":0,"separators":null,"languages":null,"parser_engine_rules":null,"extension":{"array":[],"null":null}}`, updated["chunking_config"].(string))

}

func TestPausedLibrarySettingsSaveOnlyModelBindings(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(t.Context(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, EnsurePersonalSpace(ctx, db))
	for _, model := range []types.Model{
		{ID: "old-gen", Type: types.ModelTypeGeneration},
		{ID: "new-gen", Type: types.ModelTypeGeneration},
		{ID: "new-embed", Type: types.ModelTypeEmbedding},
	} {
		model.Name, model.SpaceID, model.Source, model.Status = model.ID, types.SingleSpaceID, types.ModelSourceRemote, types.ModelStatusActive
		require.NoError(t, db.Create(&model).Error)
	}
	svc := &libraryService{db: db, repo: repository.NewLibraryRepository(db)}
	library, err := svc.CreateLibrary(ctx, types.CreateLibraryRequest{Name: "Shelf", Directory: "shelf", EntityMode: types.EntityModeManaged, ShelfEnabled: true})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE libraries SET summary_model_id='old-gen', model_tiers='{"generation":"old-gen","extension":{"keep":true}}', chunking_config='{"extension":null}' WHERE id=?`, library.ID).Error)
	require.NoError(t, maintenanceops.Pause(ctx, db, "snapshot", "restore", nil))

	otherFields := func() string {
		t.Helper()
		var raw string
		require.NoError(t, db.Raw(`SELECT (to_jsonb(libraries) - 'model_tiers' - 'summary_model_id' - 'updated_at')::text FROM libraries WHERE id=?`, library.ID).Scan(&raw).Error)
		return raw
	}
	before := otherFields()
	newTiers := &types.ModelTierConfig{Generation: "new-gen"}
	saved, err := svc.ConfigureLibrary(ctx, library.ID, types.LibraryPatch{ModelTiers: newTiers})
	require.NoError(t, err)
	require.Equal(t, "new-gen", saved.SummaryModelID)
	saved, err = svc.UpdateLibrary(ctx, library.ID, library.Name, library.Description, &types.LibraryConfig{ModelTiers: &types.ModelTierConfig{
		Generation: "new-gen", Embedding: "new-embed",
	}})
	require.NoError(t, err)
	require.Equal(t, "new-embed", saved.EmbeddingModelID)
	var raw string
	require.NoError(t, db.Raw(`SELECT model_tiers::text FROM libraries WHERE id=?`, library.ID).Scan(&raw).Error)
	require.JSONEq(t, `{"generation":"new-gen","embedding":"new-embed","extension":{"keep":true}}`, raw)

	_, err = svc.ConfigureLibrary(ctx, library.ID, types.LibraryPatch{ShelfConfig: &types.ShelfConfig{}})
	var app *apperrors.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, http.StatusServiceUnavailable, app.HTTPCode)
	_, err = svc.UpdateLibrary(ctx, library.ID, library.Name, library.Description, &types.LibraryConfig{ModelTiers: newTiers, ChunkingConfig: &types.ChunkingConfig{ChunkSize: 64}})
	require.ErrorAs(t, err, &app)
	require.Equal(t, http.StatusServiceUnavailable, app.HTTPCode)
	require.Contains(t, app.Message, "请单独提交模型档位")
	require.Equal(t, before, otherFields())
	current, err := svc.GetLibraryByID(ctx, library.ID)
	require.NoError(t, err)
	require.Equal(t, "new-embed", current.EmbeddingModelID)
	var changesets int64
	require.NoError(t, db.Model(&types.ShelfChangeset{}).Where("library_id=?", library.ID).Count(&changesets).Error)
	require.Zero(t, changesets)
}
