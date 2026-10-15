package handler

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type stubArticleLibraryService struct {
	interfaces.LibraryService
}

func (s *stubArticleLibraryService) GetLibraryByID(_ context.Context, id string) (*types.Library, error) {
	if id != "library-1" {
		return nil, errors.New("library not found")
	}
	return &types.Library{ID: "library-1", SpaceID: 1, Type: types.LibraryTypeDocument}, nil
}

func (s *stubArticleLibraryService) GetSystemLibrary(ctx context.Context, roles ...string) (*types.Library, error) {
	return s.GetLibraryByID(ctx, "library-1")
}
