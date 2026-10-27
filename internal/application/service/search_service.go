package service

import (
	"gorm.io/gorm"

	"reliquary/internal/types/interfaces"
)

type searchService struct {
	libraryService interfaces.LibraryService
	spaceService   interfaces.SpaceService
	modelService   interfaces.ModelService
	db             *gorm.DB
	vault          *VaultService
}

func (s *searchService) SetVault(v *VaultService) { s.vault = v }

func NewSearchService(libraryService interfaces.LibraryService, spaceService interfaces.SpaceService, modelService interfaces.ModelService, db *gorm.DB) interfaces.SearchService {
	return &searchService{
		libraryService: libraryService, spaceService: spaceService,
		modelService: modelService, db: db,
	}
}
