package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSystemLibrariesInitializationAndCatalog(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, EnsurePersonalSpace(ctx, db))
	_, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	require.NoError(t, err)
	repo := repository.NewLibraryRepository(db)
	svc := &libraryService{db: db, repo: repo}
	rows, err := svc.ListLibraries(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for _, want := range types.SystemLibraryDefaults() {
		got, err := repo.GetSystemLibraryByRole(ctx, 1, want.SystemRole)
		require.NoError(t, err)
		require.Equal(t, want.Name, got.Name)
		require.Equal(t, want.Template, got.Template)
		require.Equal(t, want.Directory, got.Directory)
		require.Equal(t, want.Description, got.Description)
		require.Equal(t, want.ReviewCriteria, got.ReviewCriteria)
		require.Equal(t, want.SourceEvidenceRule, got.SourceEvidenceRule)
		require.Equal(t, want.FeedEnabled, got.FeedEnabled)
		for i := range want.ContentTypes {
			want.ContentTypes[i].LibraryID = got.ID
		}
		for i := range want.RelationTypes {
			want.RelationTypes[i].LibraryID = got.ID
		}
		require.Equal(t, want.ContentTypes, got.ContentTypes)
		require.Equal(t, want.RelationTypes, got.RelationTypes)
		require.Error(t, svc.DeleteLibrary(ctx, got.ID))
	}
	institutional, err := repo.GetSystemLibraryByRole(ctx, 1, "official")
	require.NoError(t, err)
	name := "用户改名"
	off := false
	empty := []types.LibraryContentType{}
	_, err = svc.ConfigureLibrary(ctx, institutional.ID, types.LibraryPatch{Name: &name, FeedEnabled: &off, ContentTypes: &empty})
	require.NoError(t, err)
	for range 3 {
		_, err = repository.EnsureSystemLibraries(ctx, db, 1, "")
		require.NoError(t, err)
	}
	got, err := repo.GetLibraryByID(ctx, institutional.ID)
	require.NoError(t, err)
	require.Equal(t, name, got.Name)
	require.False(t, got.FeedEnabled)
	require.Empty(t, got.ContentTypes)
	require.Equal(t, name, got.DisplayName("en"))
	for _, template := range []string{types.TemplateExternalTimely, types.TemplateExternalDurable, types.TemplateOwn, types.TemplateGenerated} {
		created, err := svc.CreateLibrary(ctx, types.CreateLibraryRequest{Template: template, Directory: "custom-" + map[string]string{types.TemplateExternalTimely: "timely", types.TemplateExternalDurable: "durable", types.TemplateOwn: "own", types.TemplateGenerated: "generated"}[template], Name: template})
		require.NoError(t, err)
		require.Empty(t, created.ContentTypes)
		require.Empty(t, created.RelationTypes)
		_, err = svc.CreateLibrary(ctx, types.CreateLibraryRequest{Template: template, Directory: created.Directory, Name: "duplicate"})
		require.Error(t, err)
		for _, patch := range []types.LibraryPatch{{Template: &template}, {Directory: &created.Directory}, {SystemRole: &name}} {
			_, err = svc.ConfigureLibrary(ctx, created.ID, patch)
			require.Error(t, err)
		}
		require.NoError(t, svc.DeleteLibrary(ctx, created.ID))
	}
	for _, directory := range []string{"", "../mine", "Upper", "with space", "x/y"} {
		_, err = svc.CreateLibrary(ctx, types.CreateLibraryRequest{Template: types.TemplateOwn, Directory: directory, Name: "bad"})
		require.Error(t, err)
	}
	_, err = svc.CreateLibrary(ctx, types.CreateLibraryRequest{Template: "invalid", Directory: "valid", Name: "bad"})
	require.Error(t, err)
	custom, err := svc.CreateLibrary(ctx, types.CreateLibraryRequest{Template: types.TemplateOwn, Directory: "occupied", Name: "occupied"})
	require.NoError(t, err)
	entry := &types.Entry{ID: "immutable", SpaceID: 1, LibraryID: custom.ID, Type: "manual", SourceKind: "article", Title: "Existing"}
	require.NoError(t, db.Create(entry).Error)
	require.Error(t, svc.DeleteLibrary(ctx, custom.ID))
	entries := repository.NewEntryRepository(db)
	changed := *entry
	changed.LibraryID = institutional.ID
	entrySvc := &entryService{db: db, repo: entries}
	require.ErrorContains(t, entrySvc.UpdateEntry(ctx, &changed), "immutable")
	require.ErrorContains(t, entries.UpdateEntry(ctx, &changed), "immutable")
	require.ErrorContains(t, entries.UpdateEntryBatch(ctx, []*types.Entry{&changed}), "immutable")
	require.ErrorContains(t, entries.UpdateEntryColumn(ctx, entry.ID, "library_id", institutional.ID), "immutable")
	require.ErrorContains(t, entries.UpdateEntryColumns(ctx, entry.ID, map[string]any{"library_id": institutional.ID}), "immutable")
	_, err = entries.UpdateActiveDeletingEntryColumns(ctx, entry.ID, map[string]any{"library_id": institutional.ID})
	require.ErrorContains(t, err, "immutable")
	require.Error(t, db.Model(entry).Update("library_id", institutional.ID).Error)
	require.NoError(t, db.First(entry, "id=?", entry.ID).Error)
	require.Equal(t, custom.ID, entry.LibraryID)
}

func TestSpaceEmbeddingSelectionIsSharedByEveryLibrary(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	require.NoError(t, EnsurePersonalSpace(ctx, db))
	require.NoError(t, db.Model(&types.Space{}).Where("id=?", 1).Update("embedding_model_id", "space-vector").Error)
	require.NoError(t, db.Model(&types.Library{}).Where("space_id=?", 1).Update("embedding_model_id", "obsolete-library-vector").Error)
	// A stale general space edit must not overwrite the separately owned model selection.
	require.NoError(t, repository.NewSpaceRepository(db).UpdateSpace(ctx, &types.Space{ID: 1, Name: "Renamed space", EmbeddingModelID: "stale-vector"}))
	libraries, err := repository.NewLibraryRepository(db).ListLibraries(ctx, 1)
	require.NoError(t, err)
	require.Len(t, libraries, 5)
	for _, library := range libraries {
		id, err := ResolveModelTier(ctx, db, library, types.ModelTierEmbedding)
		require.NoError(t, err)
		require.Equal(t, "space-vector", id)
		require.Equal(t, id, library.EmbeddingModelID)
		require.Equal(t, id, library.ModelTiers.Embedding)
	}
}
