package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var errCapturedInternalKnowledgeBase = errors.New("captured internal knowledge base creation")

type internalDomainKBService struct {
	interfaces.KnowledgeBaseService
	input *types.KnowledgeBase
}

func (s *internalDomainKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "source", DomainID: "custom-source-domain", EmbeddingModelID: "embedding", SummaryModelID: "chat"}, nil
}

func (s *internalDomainKBService) CreateKnowledgeBase(_ context.Context, kb *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	s.input = kb
	return nil, errCapturedInternalKnowledgeBase
}

type internalDomainModelService struct{ interfaces.ModelService }

func (internalDomainModelService) ListModels(context.Context) ([]*types.Model, error) {
	return []*types.Model{{ID: "embedding", Type: types.ModelTypeEmbedding}, {ID: "chat", Type: types.ModelTypeKnowledgeQA}}, nil
}

func TestInternalKnowledgeBaseCreationAssignsDomain(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	for _, testcase := range []struct {
		name       string
		domainID   string
		temporary  bool
		evaluation bool
		sourceID   string
	}{
		{name: "evaluation_without_source", domainID: BuiltinAIKnowledgeDomainID, evaluation: true},
		{name: "evaluation_with_source", domainID: "custom-source-domain", evaluation: true, sourceID: "source"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			kbService := &internalDomainKBService{}
			s := NewEvaluationService(nil, nil, kbService, nil, nil, internalDomainModelService{})
			_, err := s.Evaluation(ctx, "dataset", testcase.sourceID, "chat", "rerank")
			require.ErrorIs(t, err, errCapturedInternalKnowledgeBase)
			require.NotNil(t, kbService.input)
			require.NotEmpty(t, kbService.input.DomainID)
			require.Equal(t, testcase.domainID, kbService.input.DomainID)
			require.Equal(t, testcase.temporary, kbService.input.IsTemporary)
			require.Equal(t, "embedding", kbService.input.EmbeddingModelID)
		})
	}
}
