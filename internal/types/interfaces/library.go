package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// LibraryService provides settings and retrieval for the sole library.
type LibraryService interface {
	GetLibrary(ctx context.Context) (*types.Library, error)
	GetLibraryByID(ctx context.Context, id string) (*types.Library, error)
	GetLibraryByIDOnly(ctx context.Context, id string) (*types.Library, error)
	GetLibrariesByIDsOnly(ctx context.Context, ids []string) ([]*types.Library, error)
	FillLibraryCounts(ctx context.Context, library *types.Library) error
	UpdateLibrary(ctx context.Context, id, name, description string, config *types.LibraryConfig) (*types.Library, error)
	HybridSearch(ctx context.Context, id string, params types.SearchParams) ([]*types.SearchResult, error)
	GetQueryEmbedding(ctx context.Context, libraryID, queryText string) ([]float32, error)
	ResolveEmbeddingModelKeys(ctx context.Context, libraries []*types.Library) map[string]string
}

// LibraryRepository preserves the internal library identity and model bindings.
type LibraryRepository interface {
	CreateLibrary(ctx context.Context, library *types.Library) error
	GetLibraryBySpaceID(ctx context.Context, spaceID uint64) (*types.Library, error)
	GetLibraryByID(ctx context.Context, id string) (*types.Library, error)
	GetLibraryByIDAndSpace(ctx context.Context, id string, spaceID uint64) (*types.Library, error)
	GetLibraryByIDs(ctx context.Context, ids []string) ([]*types.Library, error)
	UpdateLibrary(ctx context.Context, library *types.Library) error
	CountByModelID(ctx context.Context, spaceID uint64, modelID string) (int64, error)
	ListModelUsages(ctx context.Context, spaceID uint64, modelID string) ([]types.ModelUsageResource, error)
}
