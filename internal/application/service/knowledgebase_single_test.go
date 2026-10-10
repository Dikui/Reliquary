package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSingleKnowledgeBaseRegistrationStartupAndConstraint(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	require.NoError(t, EnsureSingleTeamSpace(ctx, db))
	tenants := NewTenantService(repository.NewTenantRepository(db))
	users := NewUserService(&config.Config{}, repository.NewUserRepository(db), repository.NewAuthTokenRepository(db), tenants)
	user, err := users.Register(ctx, &types.RegisterRequest{Username: "personal", Email: "kb@example.test", Password: "initial-password"})
	require.NoError(t, err)
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	kb, err := kbRepo.GetKnowledgeBaseByTenantID(ctx, types.SingleTenantID)
	require.NoError(t, err)
	require.Equal(t, user.ID, kb.CreatorID)
	require.Contains(t, []string{"知识库", "Knowledge Base"}, kb.Name)
	require.Equal(t, types.KnowledgeBaseTypeDocument, kb.Type)
	for range 3 {
		require.NoError(t, EnsureSingleKnowledgeBase(ctx, db))
	}
	same, err := kbRepo.GetKnowledgeBaseByTenantID(ctx, types.SingleTenantID)
	require.NoError(t, err)
	require.Equal(t, kb.ID, same.ID)
	second := &types.KnowledgeBase{ID: "second", TenantID: types.SingleTenantID, Name: "Second"}
	second.EnsureDefaults()
	require.ErrorContains(t, db.Create(second).Error, "idx_knowledge_bases_active_tenant")
	var count int64
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Delete(kb).Error)
	require.NoError(t, EnsureSingleKnowledgeBase(ctx, db))
	repaired, err := kbRepo.GetKnowledgeBaseByTenantID(ctx, types.SingleTenantID)
	require.NoError(t, err)
	require.NotEqual(t, kb.ID, repaired.ID)
	require.Equal(t, user.ID, repaired.CreatorID)
	require.NoError(t, EnsureSingleKnowledgeBase(ctx, db))
	require.NoError(t, db.Model(&types.KnowledgeBase{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Unscoped().Model(&types.KnowledgeBase{}).Count(&count).Error)
	require.EqualValues(t, 2, count, "the partial unique index permits replacement of a deleted knowledge base")
}
