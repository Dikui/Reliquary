package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type teamKnowledgeBaseRepo struct {
	interfaces.KnowledgeBaseRepository
	rows []*types.KnowledgeBase
}

func (r *teamKnowledgeBaseRepo) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	for _, kb := range r.rows {
		if kb.ID == id {
			return kb, nil
		}
	}
	return nil, repository.ErrKnowledgeBaseNotFound
}

func (r *teamKnowledgeBaseRepo) GetKnowledgeBaseByIDs(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return r.rows, nil
}

func TestKnowledgeBaseLookupsRejectObjectsOutsideTeam(t *testing.T) {
	teamKB := &types.KnowledgeBase{ID: "team-kb", TenantID: types.SingleTenantID, CreatorID: "another-member"}
	foreignKB := &types.KnowledgeBase{ID: "foreign-kb", TenantID: 2}
	svc := &knowledgeBaseService{repo: &teamKnowledgeBaseRepo{rows: []*types.KnowledgeBase{teamKB, foreignKB}}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	for _, get := range []func(context.Context, string) (*types.KnowledgeBase, error){svc.GetKnowledgeBaseByID, svc.GetKnowledgeBaseByIDOnly} {
		got, err := get(ctx, teamKB.ID)
		require.NoError(t, err)
		require.Equal(t, teamKB.ID, got.ID)
		_, err = get(ctx, foreignKB.ID)
		require.ErrorIs(t, err, repository.ErrKnowledgeBaseNotFound)
	}
	got, err := svc.GetKnowledgeBasesByIDsOnly(ctx, []string{teamKB.ID, foreignKB.ID})
	require.NoError(t, err)
	require.Equal(t, []*types.KnowledgeBase{teamKB}, got)
}

func TestQARetrievalAlwaysUsesSingleTeam(t *testing.T) {
	svc := &sessionService{}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2))
	req := &types.QARequest{Session: &types.Session{TenantID: 2}, CustomAgent: &types.CustomAgent{TenantID: 2}}
	require.Equal(t, types.SingleTenantID, svc.resolveRetrievalTenantID(ctx, req))
}
