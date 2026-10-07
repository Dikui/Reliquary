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

func TestDeleteDocumentTagRequiresTeamEditor(t *testing.T) {
	repo := &safeDeleteTagRepo{}
	service := &knowledgeTagService{repo: repo, kbService: &safeDeleteKBService{}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	other := context.WithValue(ctx, types.UserIDContextKey, "owner")
	other = context.WithValue(other, types.TenantRoleContextKey, types.TenantRoleViewer)
	require.Error(t, service.DeleteTag(other, "tag"))
	require.False(t, repo.deleted)
	owner := context.WithValue(ctx, types.UserIDContextKey, "other-editor")
	owner = context.WithValue(owner, types.TenantRoleContextKey, types.TenantRoleContributor)
	require.NoError(t, service.DeleteTag(owner, "tag"))
	require.True(t, repo.deleted)
}

func TestDeleteDocumentTagHonorsTeamAPIKeyScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scope   types.TenantAPIKeyScope
		allowed bool
	}{
		{"ingestion", types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}, KnowledgeBaseIDs: types.StringArray{"kb"}}, true},
		{"full access", types.TenantAPIKeyScope{FullAccess: true}, true},
		{"retrieval", types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityRetrieve)}}, false},
		{"outside scope", types.TenantAPIKeyScope{Capabilities: types.StringArray{string(types.APIKeyCapabilityIngest)}, KnowledgeBaseIDs: types.StringArray{"other-kb"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &safeDeleteTagRepo{}
			svc := &knowledgeTagService{repo: repo, kbService: &safeDeleteKBService{}}
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
			ctx = types.WithTenantAPIKeyScope(ctx, tc.scope)
			err := svc.DeleteTag(ctx, "tag")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.allowed, repo.deleted)
		})
	}
}
