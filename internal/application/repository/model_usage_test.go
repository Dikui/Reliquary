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

func TestCountByModelID_KnowledgeBase(t *testing.T) {
	ctx := context.Background()
	db := setupKBTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	modelID := "embed-model-1"

	kb := makeKB()
	kb.EmbeddingModelID = modelID
	require.NoError(t, db.Create(kb).Error)

	count, err := repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	count, err = repo.CountByModelID(ctx, 1, "other-model")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	kb2 := makeKB()
	kb2.ID = uuid.New().String()
	kb2.ModelTiers.Generation = modelID
	require.NoError(t, db.Create(kb2).Error)

	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	require.NoError(t, db.Delete(kb2).Error)
	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestListModelUsages_KnowledgeBase(t *testing.T) {
	ctx := context.Background()
	db := setupKBTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	modelID := "all-kb-bindings"

	allBindings := makeKB()
	allBindings.Name = "Zulu KB"
	allBindings.EmbeddingModelID = modelID
	allBindings.SummaryModelID = modelID
	allBindings.ModelTiers.Generation = modelID
	allBindings.ModelTiers.Light = modelID
	require.NoError(t, db.Create(allBindings).Error)

	generationOnly := makeKB()
	generationOnly.Name = "Alpha KB"
	generationOnly.ModelTiers.Generation = modelID
	require.NoError(t, db.Create(generationOnly).Error)

	otherSpace := makeKB()
	otherSpace.Name = "Other space"
	otherSpace.SpaceID = 2
	otherSpace.EmbeddingModelID = modelID
	require.NoError(t, db.Create(otherSpace).Error)

	deleted := makeKB()
	deleted.Name = "Deleted KB"
	deleted.EmbeddingModelID = modelID
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, "Alpha KB", usages[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBinding("generation_model_tier")}, usages[0].Bindings)
	assert.Equal(t, "Zulu KB", usages[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{
		types.ModelUsageBinding("light_model_tier"),
		types.ModelUsageBinding("generation_model_tier"),
		types.ModelUsageBindingEmbeddingModel,
		types.ModelUsageBindingSummaryModel,
	}, usages[1].Bindings)
}

func TestListModelUsages_KnowledgeBaseRespectsLimit(t *testing.T) {
	ctx := context.Background()
	db := setupKBTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	modelID := "shared-embed"

	for i := 0; i < types.ModelUsageListLimit+3; i++ {
		kb := makeKB()
		kb.Name = fmt.Sprintf("KB %03d", i)
		kb.EmbeddingModelID = modelID
		require.NoError(t, db.Create(kb).Error)
	}

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, types.ModelUsageListLimit)
	assert.Equal(t, "KB 000", usages[0].Name)

	count, err := repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(types.ModelUsageListLimit+3), count)
}
