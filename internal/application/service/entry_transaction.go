package service

import (
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"

	"gorm.io/gorm"
)

func (s *entryService) transactionService(tx *gorm.DB) *entryService {
	scoped := *s
	scoped.db = tx
	scoped.repo = repository.NewEntryRepository(tx)
	scoped.chunkRepo = repository.NewChunkRepository(tx)
	scoped.spaceRepo = repository.NewSpaceRepository(tx)
	scoped.retrieveEngine = retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))
	scoped.chunkService = NewChunkService(
		scoped.chunkRepo,
		scoped.repo,
		repository.NewLibraryRepository(tx),
		scoped.modelService,
		scoped.retrieveEngine,
	)
	scoped.vault, scoped.resourceCatalog = nil, nil
	return &scoped
}
