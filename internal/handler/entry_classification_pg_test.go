package handler

import (
	"context"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type classificationEntryService struct {
	interfaces.EntryService
	repo interfaces.EntryRepository
}

func (s *classificationEntryService) ListPagedEntryByLibraryID(ctx context.Context, libraryID string, page *types.Pagination, filter types.EntryListFilter) (*types.PageResult, error) {
	rows, total, err := s.repo.ListPagedEntryByLibraryID(ctx, uint64(1), libraryID, page, filter)
	return &types.PageResult{Data: rows, Total: total, Page: page.Page, PageSize: page.PageSize}, err
}
