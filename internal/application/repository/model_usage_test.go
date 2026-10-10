package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

const customAgentsTestDDL = `
CREATE TABLE IF NOT EXISTS custom_agents (
    id VARCHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    avatar VARCHAR(64),
    is_builtin BOOLEAN NOT NULL DEFAULT false,
    tenant_id INTEGER NOT NULL,
    created_by VARCHAR(36),
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (id, tenant_id)
);
`

func setupModelUsageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupKBTestDB(t)
	require.NoError(t, db.Exec(customAgentsTestDDL).Error)
	return db
}

func TestCountByModelID_KnowledgeBase(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
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
	kb2.VLMConfig = types.VLMConfig{Enabled: true, ModelID: modelID}
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
	db := setupModelUsageTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	modelID := "all-kb-bindings"

	allBindings := makeKB()
	allBindings.Name = "Zulu KB"
	allBindings.EmbeddingModelID = modelID
	allBindings.SummaryModelID = modelID
	allBindings.ImageProcessingConfig = types.ImageProcessingConfig{ModelID: modelID}
	allBindings.VLMConfig = types.VLMConfig{Enabled: true, ModelID: modelID}
	require.NoError(t, db.Create(allBindings).Error)

	vlmOnly := makeKB()
	vlmOnly.Name = "Alpha KB"
	vlmOnly.VLMConfig = types.VLMConfig{Enabled: true, ModelID: modelID}
	require.NoError(t, db.Create(vlmOnly).Error)

	otherTenant := makeKB()
	otherTenant.Name = "Other tenant"
	otherTenant.TenantID = 2
	otherTenant.EmbeddingModelID = modelID
	require.NoError(t, db.Create(otherTenant).Error)

	deleted := makeKB()
	deleted.Name = "Deleted KB"
	deleted.EmbeddingModelID = modelID
	require.NoError(t, db.Create(deleted).Error)
	require.NoError(t, db.Delete(deleted).Error)

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, "Alpha KB", usages[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}, usages[0].Bindings)
	assert.Equal(t, "Zulu KB", usages[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{
		types.ModelUsageBindingEmbeddingModel,
		types.ModelUsageBindingSummaryModel,
		types.ModelUsageBindingImageProcessingModel,
		types.ModelUsageBindingVLMModel,
	}, usages[1].Bindings)
}

func TestListModelUsages_KnowledgeBaseRespectsLimit(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
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

func TestCountByModelID_CustomAgent(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
	repo := NewCustomAgentRepository(db)
	modelID := "chat-model-1"

	agent := &types.CustomAgent{
		ID:       uuid.New().String(),
		Name:     "test-agent",
		TenantID: 1,
		Config: types.CustomAgentConfig{
			ModelID: modelID,
		},
	}
	require.NoError(t, repo.CreateAgent(ctx, agent))

	count, err := repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	agent2 := &types.CustomAgent{
		ID:       uuid.New().String(),
		Name:     "rerank-agent",
		TenantID: 1,
		Config: types.CustomAgentConfig{
			RerankModelID: modelID,
		},
	}
	require.NoError(t, repo.CreateAgent(ctx, agent2))

	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	count, err = repo.CountByModelID(ctx, 2, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	require.NoError(t, repo.DeleteAgent(ctx, agent2.ID, 1))
	count, err = repo.CountByModelID(ctx, 1, modelID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestListModelUsages_CustomAgent(t *testing.T) {
	ctx := context.Background()
	db := setupModelUsageTestDB(t)
	repo := NewCustomAgentRepository(db)
	modelID := "all-agent-bindings"

	allBindings := &types.CustomAgent{
		ID:       uuid.New().String(),
		Name:     "Zulu agent",
		TenantID: 1,
		Config: types.CustomAgentConfig{
			ModelID:                modelID,
			RerankModelID:          modelID,
			VLMModelID:             modelID,
			QueryUnderstandModelID: modelID,
		},
	}
	require.NoError(t, repo.CreateAgent(ctx, allBindings))

	vlmOnly := &types.CustomAgent{
		ID:       uuid.New().String(),
		Name:     "Alpha agent",
		TenantID: 1,
		Config:   types.CustomAgentConfig{VLMModelID: modelID},
	}
	require.NoError(t, repo.CreateAgent(ctx, vlmOnly))

	otherTenant := &types.CustomAgent{
		ID: uuid.New().String(), Name: "Other tenant", TenantID: 2,
		Config: types.CustomAgentConfig{ModelID: modelID},
	}
	require.NoError(t, repo.CreateAgent(ctx, otherTenant))

	deleted := &types.CustomAgent{
		ID: uuid.New().String(), Name: "Deleted agent", TenantID: 1,
		Config: types.CustomAgentConfig{ModelID: modelID},
	}
	require.NoError(t, repo.CreateAgent(ctx, deleted))
	require.NoError(t, repo.DeleteAgent(ctx, deleted.ID, deleted.TenantID))

	usages, err := repo.ListModelUsages(ctx, 1, modelID)
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.Equal(t, "Alpha agent", usages[0].Name)
	assert.Equal(t, []types.ModelUsageBinding{types.ModelUsageBindingVLMModel}, usages[0].Bindings)
	assert.Equal(t, "Zulu agent", usages[1].Name)
	assert.Equal(t, []types.ModelUsageBinding{
		types.ModelUsageBindingChatModel,
		types.ModelUsageBindingRerankModel,
		types.ModelUsageBindingVLMModel,
		types.ModelUsageBindingQueryUnderstandModel,
	}, usages[1].Bindings)
}
