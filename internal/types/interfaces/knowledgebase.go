package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// KnowledgeBaseService provides settings and retrieval for the sole knowledge base.
type KnowledgeBaseService interface {
	GetKnowledgeBase(ctx context.Context) (*types.KnowledgeBase, error)
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
	GetKnowledgeBaseByIDOnly(ctx context.Context, id string) (*types.KnowledgeBase, error)
	GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error)
	FillKnowledgeBaseCounts(ctx context.Context, kb *types.KnowledgeBase) error
	UpdateKnowledgeBase(ctx context.Context, id, name, description string, config *types.KnowledgeBaseConfig) (*types.KnowledgeBase, error)
	HybridSearch(ctx context.Context, id string, params types.SearchParams) ([]*types.SearchResult, error)
	GetQueryEmbedding(ctx context.Context, kbID, queryText string) ([]float32, error)
	ResolveEmbeddingModelKeys(ctx context.Context, kbs []*types.KnowledgeBase) map[string]string
}

// KnowledgeBaseRepository preserves the internal knowledge base identity and model bindings.
type KnowledgeBaseRepository interface {
	CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error
	GetKnowledgeBaseBySpaceID(ctx context.Context, spaceID uint64) (*types.KnowledgeBase, error)
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
	GetKnowledgeBaseByIDAndSpace(ctx context.Context, id string, spaceID uint64) (*types.KnowledgeBase, error)
	GetKnowledgeBaseByIDs(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error)
	UpdateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error
	CountByModelID(ctx context.Context, spaceID uint64, modelID string) (int64, error)
	ListModelUsages(ctx context.Context, spaceID uint64, modelID string) ([]types.ModelUsageResource, error)
}
