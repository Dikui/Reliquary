package container

import (
	"gorm.io/gorm"
	postgresRepo "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/types/interfaces"
)

// NewPostgresRetrievalService uses the application database for retrieval.
func NewPostgresRetrievalService(db *gorm.DB) interfaces.RetrievalService {
	return retriever.NewPostgresRetrievalService(postgresRepo.NewPostgresRetrieveEngineRepository(db))
}
