package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type searchTargetKnowledgeService struct {
	interfaces.KnowledgeService
	knowledges []*types.Knowledge
}

func (s *searchTargetKnowledgeService) GetKnowledgeBatch(_ context.Context, _ uint64, ids []string) ([]*types.Knowledge, error) {
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	var out []*types.Knowledge
	for _, knowledge := range s.knowledges {
		if allowed[knowledge.ID] {
			out = append(out, knowledge)
		}
	}
	return out, nil
}

// This retrieval assertion previously lived with the deleted session tests.
func TestBuildSearchTargets_ExplicitKnowledgeScopeDisablesRecallThresholds(t *testing.T) {
	svc := &knowledgeSearchService{knowledgeService: &searchTargetKnowledgeService{
		knowledges: []*types.Knowledge{{ID: "doc-1", TenantID: 100, KnowledgeBaseID: "doc-kb"}},
	}}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(100))
	targets, err := svc.buildSearchTargets(ctx, 100, nil, []string{"doc-1"})

	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, types.SearchTargetTypeKnowledge, targets[0].Type)
	assert.Equal(t, []string{"doc-1"}, targets[0].KnowledgeIDs)
	assert.True(t, targets[0].DisableRecallThresholds)
}
