package service

import (
	"context"
	stderrors "errors"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// fakeKBRepo stores settings for service validation tests.
type fakeKBRepo struct {
	rows      map[string]*types.KnowledgeBase
	createErr error
}

func newFakeKBRepo() *fakeKBRepo { return &fakeKBRepo{rows: map[string]*types.KnowledgeBase{}} }

func (r *fakeKBRepo) CreateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.rows[kb.ID] = kb
	return nil
}
func (r *fakeKBRepo) GetKnowledgeBaseBySpaceID(ctx context.Context, spaceID uint64) (*types.KnowledgeBase, error) {
	for _, kb := range r.rows {
		if kb.SpaceID == spaceID {
			return kb, nil
		}
	}
	return nil, stderrors.New("not found")
}
func (r *fakeKBRepo) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return r.rows[id], nil
}
func (r *fakeKBRepo) GetKnowledgeBaseByIDAndSpace(_ context.Context, id string, spaceID uint64) (*types.KnowledgeBase, error) {
	kb := r.rows[id]
	if kb == nil || kb.SpaceID != spaceID {
		return nil, stderrors.New("not found")
	}
	return kb, nil
}
func (r *fakeKBRepo) GetKnowledgeBaseByIDs(_ context.Context, _ []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}
func (r *fakeKBRepo) UpdateKnowledgeBase(_ context.Context, _ *types.KnowledgeBase) error {
	return nil
}
func (r *fakeKBRepo) CountByModelID(_ context.Context, _ uint64, _ string) (int64, error) {
	return 0, nil
}

func (r *fakeKBRepo) ListModelUsages(_ context.Context, _ uint64, _ string) ([]types.ModelUsageResource, error) {
	return []types.ModelUsageResource{}, nil
}

// Force compile-time conformance check so any future interface change
// surfaces as a build error here rather than at usage site.
var _ interfaces.KnowledgeBaseRepository = (*fakeKBRepo)(nil)
