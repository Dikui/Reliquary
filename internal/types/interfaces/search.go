package interfaces

import (
	"context"
	"reliquary/internal/types"
)

type SearchService interface {
	Search(ctx context.Context, request types.EntrySearchRequest) (*types.EntrySearchResponse, error)
	Options(ctx context.Context) (*types.EntrySearchOptions, error)
}
