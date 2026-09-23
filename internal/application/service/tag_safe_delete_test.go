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

func TestDeleteDocumentTagRequiresCreatorEvenWhenForced(t *testing.T) {
	repo := &safeDeleteTagRepo{}
	service := &knowledgeTagService{repo: repo, kbService: &safeDeleteKBService{}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	other := context.WithValue(ctx, types.UserIDContextKey, "other")
	require.Error(t, service.DeleteTag(other, "tag", true, false, nil))
	require.False(t, repo.deleted)
	owner := context.WithValue(ctx, types.UserIDContextKey, "owner")
	require.NoError(t, service.DeleteTag(owner, "tag", true, false, nil))
	require.True(t, repo.deleted)
}
