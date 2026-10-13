package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestCountByModelID_Library(t *testing.T) {
	ctx := context.Background()
	db := setupLibraryTestDB(t)
	repo := NewLibraryRepository(db)
	modelID := "embed-model-1"

	library := makeLibrary()
	library.EmbeddingModelID = modelID
	require.NoError(t, db.Create(library).Error)

	count, err := repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	count, err = repo.CountByModelID(ctx, 1, "other-model")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	library2 := makeLibrary()
	library2.ID = uuid.New().String()
	library2.ModelTiers.Generation = modelID
	require.NoError(t, db.Create(library2).Error)

	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	require.NoError(t, db.Delete(library2).Error)
	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestListModelUsages_Library(t *testing.T) {
	ctx := context.Background()
	db := setupLibraryTestDB(t)
	repo := NewLibraryRepository(db)
	modelID := "all-library-bindings"

	allBindings := makeLibrary()
	allBindings.Name = "Zulu Library"
	allBindings.EmbeddingModelID = modelID
	allBindings.SummaryModelID = modelID
	allBindings.ModelTiers.Generation = modelID
	allBindings.ModelTiers.Light = modelID
	require.NoError(t, db.Create(allBindings).Error)

	generationOnly := makeLibrary()
	generationOnly.Name = "Alpha Library"
	generationOnly.ModelTiers.Generation = modelID
	require.NoError(t, db.Create(generationOnly).Error)

	otherSpace := makeLibrary()
	otherSpace.Name = "Other space"
	otherSpace.SpaceID = 2
	otherSpace.EmbeddingModelID = modelID
	require.NoError(t, db.Create(otherSpace).Error)

	deleted := makeLibrary()
	deleted.Name = "Deleted Library"
	deleted.EmbeddingModelID = modelID
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, "Alpha Library", usages[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}, usages[0].Bindings)
	assert.Equal(t, "Zulu Library", usages[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{
		types.ModelUsageBinding("light_model_tier"),
		types.ModelUsageBinding("generation_model_tier"),
		types.ModelUsageBindingEmbeddingModel,
		types.ModelUsageBindingSummaryModel,
	}, usages[1].Bindings)
}

func TestListModelUsages_LibraryRespectsLimit(t *testing.T) {
	ctx := context.Background()
	db := setupLibraryTestDB(t)
	repo := NewLibraryRepository(db)
	modelID := "shared-embed"

	for i := 0; i < types.ModelUsageListLimit+3; i++ {
		library := makeLibrary()
		library.Name = fmt.Sprintf("Library %03d", i)
		library.EmbeddingModelID = modelID
		require.NoError(t, db.Create(library).Error)
	}

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, types.ModelUsageListLimit)
	assert.Equal(t, "Library 000", usages[0].Name)

	count, err := repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(types.ModelUsageListLimit+3), count)
}
