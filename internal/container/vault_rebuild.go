package container

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/panjf2000/ants/v2"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// RebuildVault initializes only the database and indexing dependencies.
func RebuildVault(ctx context.Context) (*service.VaultRebuildResult, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	db, err := initDatabase(cfg)
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	return rebuildVaultWithDatabase(ctx, db, cfg)
}

// RebuildVaultWithDatabase keeps maintenance output machine readable and avoids
// starting another connection or changing runtime configuration during restore.
func RebuildVaultWithDatabase(ctx context.Context, db *gorm.DB) (*service.VaultRebuildResult, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	return rebuildVaultWithDatabase(ctx, db, cfg)
}

func rebuildVaultWithDatabase(ctx context.Context, db *gorm.DB, cfg *config.Config) (*service.VaultRebuildResult, error) {
	if service.HasVaultReplica(cfg.Vault.Root) {
		var existing int64
		if err := db.WithContext(ctx).Unscoped().Model(&types.Entry{}).Count(&existing).Error; err != nil {
			return nil, err
		}
		if existing != 0 {
			return nil, fmt.Errorf("replica import requires an empty entry database")
		}
		documents, err := service.NewVaultDocumentService(db, nil, NewPostgresRetrievalService(db), cfg.Vault.Root)
		if err != nil {
			return nil, err
		}
		defer documents.Close()
		pauseCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := maintenanceops.Pause(pauseCtx, db, "snapshot", "vault rebuild awaiting configuration", nil); err != nil {
			return nil, fmt.Errorf("pause empty database before vault import: %w", err)
		}
		result, err := documents.RebuildReplica(ctx)
		if err != nil {
			return result, err
		}
		return result, nil
	}
	library, err := repository.EnsureSystemLibraries(ctx, db, types.SingleSpaceID, "")
	if err != nil {
		return nil, err
	}
	var count int64
	if err := db.WithContext(ctx).Unscoped().Model(&types.Entry{}).Count(&count).Error; err != nil {
		return nil, err
	}
	if count != 0 {
		return nil, fmt.Errorf("vault-rebuild requires an empty entry database")
	}
	if err := configureVaultRebuildEmbedding(ctx, db, library); err != nil {
		return nil, err
	}
	workers, err := ants.NewPool(5)
	if err != nil {
		return nil, err
	}
	defer workers.Release()
	ollamaClient, err := initOllamaService()
	if err != nil {
		return nil, err
	}
	models := service.NewModelServiceWithRoutingDB(repository.NewModelRepository(db), repository.NewLibraryRepository(db), ollamaClient, embedding.NewBatchEmbedder(workers), service.NewSpaceService(repository.NewSpaceRepository(db)), db)
	documents, err := service.NewVaultDocumentService(db, models, NewPostgresRetrievalService(db), cfg.Vault.Root)
	if err != nil {
		return nil, err
	}
	defer documents.Close()
	return documents.Rebuild(ctx)
}

// An empty application database can opt into vector rebuilding using an
// OpenAI-compatible embedding endpoint. Without it, keyword indexing works.
func configureVaultRebuildEmbedding(ctx context.Context, db *gorm.DB, library *types.Library) error {
	id := strings.TrimSpace(os.Getenv("VAULT_REBUILD_EMBEDDING_MODEL_ID"))
	endpoint := strings.TrimSpace(os.Getenv("VAULT_REBUILD_EMBEDDING_URL"))
	if endpoint != "" {
		name := strings.TrimSpace(os.Getenv("VAULT_REBUILD_EMBEDDING_MODEL"))
		dimension, err := strconv.Atoi(os.Getenv("VAULT_REBUILD_EMBEDDING_DIMENSION"))
		if name == "" || err != nil || dimension <= 0 || dimension > 4096 {
			return fmt.Errorf("embedding URL requires VAULT_REBUILD_EMBEDDING_MODEL and a positive VAULT_REBUILD_EMBEDDING_DIMENSION up to 4096")
		}
		model := &types.Model{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Name: name, Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{BaseURL: endpoint, APIKey: os.Getenv("VAULT_REBUILD_EMBEDDING_API_KEY"), InterfaceType: "openai", Provider: "generic", EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension}}}
		if err := db.WithContext(ctx).Create(model).Error; err != nil {
			return err
		}
		id = model.ID
	}
	if id == "" {
		if library.EmbeddingModelID == "" && library.ModelTiers.Embedding == "" {
			library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true, VectorEnabled: false}
			return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Save(library).Error; err != nil {
					return err
				}
				return tx.Model(&types.Library{}).Where("space_id=?", library.SpaceID).Update("indexing_strategy", library.IndexingStrategy).Error
			})
		}
		return nil
	}
	var model types.Model
	if err := db.WithContext(ctx).Where("id=? AND space_id=? AND type=?", id, types.SingleSpaceID, types.ModelTypeEmbedding).First(&model).Error; err != nil {
		return fmt.Errorf("rebuild embedding model: %w", err)
	}
	if err := db.WithContext(ctx).Model(&types.Space{}).Where("id=?", library.SpaceID).Update("embedding_model_id", id).Error; err != nil {
		return err
	}
	library.EmbeddingModelID = id
	library.ModelTiers.Embedding = id
	library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true, VectorEnabled: true}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(library).Error; err != nil {
			return err
		}
		return tx.Model(&types.Library{}).Where("space_id=?", library.SpaceID).Update("indexing_strategy", library.IndexingStrategy).Error
	})
}
