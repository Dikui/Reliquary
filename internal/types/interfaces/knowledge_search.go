package interfaces

import (
	"context"
	"reliquary/internal/types"
)

type KnowledgeSearchService interface {
	Search(ctx context.Context, request types.ArticleSearchRequest) (*types.ArticleSearchResponse, error)
	Options(ctx context.Context) (*types.ArticleSearchOptions, error)
}
