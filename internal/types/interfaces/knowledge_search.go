package interfaces

import (
	"context"
	"reliquary/internal/types"
)

type KnowledgeSearchService interface {
	SearchKnowledge(ctx context.Context, knowledgeBaseIDs, knowledgeIDs []string, query string) ([]*types.SearchResult, error)
}
