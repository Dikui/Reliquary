package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type safeDeleteTagRepo struct {
	interfaces.KnowledgeTagRepository
	deleted bool
}

func (r *safeDeleteTagRepo) GetByID(context.Context, uint64, string) (*types.KnowledgeTag, error) {
	return &types.KnowledgeTag{ID: "tag", TenantID: 1, KnowledgeBaseID: "kb", Kind: types.TagKindEvent}, nil
}

func (r *safeDeleteTagRepo) Delete(context.Context, uint64, string) error {
	r.deleted = true
	return nil
}

type safeDeleteKBService struct {
	interfaces.KnowledgeBaseService
}

func (*safeDeleteKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", TenantID: 1, Type: types.KnowledgeBaseTypeDocument, CreatorID: "owner"}, nil
}

func TestDeleteDocumentTagRequiresUser(t *testing.T) {
	repo := &safeDeleteTagRepo{}
	service := &knowledgeTagService{repo: repo, kbService: &safeDeleteKBService{}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	require.Error(t, service.DeleteTag(ctx, "tag"))
	require.False(t, repo.deleted)
	user := context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	otherTenant := context.WithValue(user, types.TenantIDContextKey, uint64(2))
	require.Error(t, service.DeleteTag(otherTenant, "tag"))
	require.False(t, repo.deleted)
	require.NoError(t, service.DeleteTag(user, "tag"))
	require.True(t, repo.deleted)
}
