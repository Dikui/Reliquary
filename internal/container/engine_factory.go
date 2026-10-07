package container

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	postgresRepo "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// NewEngineFactory constructs retrieval services using the application database.
func NewEngineFactory(db *gorm.DB) interfaces.EngineFactory {
	return func(ctx context.Context, store types.VectorStore) (interfaces.RetrieveEngineService, error) {
		return createEngineServiceFromStore(ctx, store, db)
	}
}

// createEngineServiceFromStore constructs a PostgreSQL retriever on the application connection.
func createEngineServiceFromStore(_ context.Context, store types.VectorStore, db *gorm.DB) (interfaces.RetrieveEngineService, error) {
	if store.EngineType != types.PostgresRetrieverEngineType {
		return nil, fmt.Errorf("unsupported engine type: %s", store.EngineType)
	}
	if !store.ConnectionConfig.UseDefaultConnection {
		return nil, fmt.Errorf("postgres retrieval requires the application database connection")
	}
	repo := postgresRepo.NewPostgresRetrieveEngineRepository(db)
	return retriever.NewKVHybridRetrieveEngine(repo, types.PostgresRetrieverEngineType), nil
}
