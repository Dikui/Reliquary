package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// ErrInvalidTenantID represents an error for invalid tenant ID
var ErrInvalidTenantID = errors.New("invalid tenant ID")

// knowledgeBaseService implements the knowledge base service interface
type knowledgeBaseService struct {
	db             *gorm.DB
	repo           interfaces.KnowledgeBaseRepository
	kgRepo         interfaces.KnowledgeRepository
	chunkRepo      interfaces.ChunkRepository
	modelService   interfaces.ModelService
	retrieveEngine interfaces.RetrievalService

	resourceCatalog        interfaces.ResourceCatalog
	nameEmbeddingRebuilder EntityNameEmbeddingRebuildTrigger
}

// NewKnowledgeBaseService creates a new knowledge base service
func NewKnowledgeBaseService(db *gorm.DB, repo interfaces.KnowledgeBaseRepository,
	kgRepo interfaces.KnowledgeRepository,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	retrieveEngine interfaces.RetrievalService,

	resourceCatalog interfaces.ResourceCatalog,
) interfaces.KnowledgeBaseService {
	return &knowledgeBaseService{
		db:             db,
		repo:           repo,
		kgRepo:         kgRepo,
		chunkRepo:      chunkRepo,
		modelService:   modelService,
		retrieveEngine: retrieveEngine,

		resourceCatalog: resourceCatalog,
	}
}

// GetKnowledgeBase returns the only active knowledge base in the personal space.
func (s *knowledgeBaseService) GetKnowledgeBase(ctx context.Context) (*types.KnowledgeBase, error) {
	kb, err := s.repo.GetKnowledgeBaseByTenantID(ctx, types.MustTenantIDFromContext(ctx))
	if err != nil {
		return nil, err
	}
	kb.EnsureDefaults()
	return kb, nil
}

// Lock the existing row while persisting knowledge base settings.
func (s *knowledgeBaseService) persistKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
	if s.db == nil {
		return s.repo.UpdateKnowledgeBase(ctx, kb)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id = ? AND id = ?", kb.TenantID, kb.ID).First(&active).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewNotFoundError("knowledge base not found")
			}
			return err
		}
		return repository.NewKnowledgeBaseRepository(tx).UpdateKnowledgeBase(ctx, kb)
	})
}

// GetKnowledgeBaseByID retrieves a knowledge base by its ID
func (s *knowledgeBaseService) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	if kb == nil || kb.TenantID != types.SingleTenantID {
		return nil, repository.ErrKnowledgeBaseNotFound
	}
	kb.EnsureDefaults()
	return kb, nil
}

func (s *knowledgeBaseService) GetKnowledgeBaseByIDOnly(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	if kb == nil || kb.TenantID != types.SingleTenantID {
		return nil, repository.ErrKnowledgeBaseNotFound
	}
	kb.EnsureDefaults()
	return kb, nil
}

// GetKnowledgeBasesByIDsOnly retrieves team knowledge bases by IDs (batch).
func (s *knowledgeBaseService) GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	kbs, err := s.repo.GetKnowledgeBaseByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	teamKBs := make([]*types.KnowledgeBase, 0, len(kbs))
	for _, kb := range kbs {
		if kb != nil && kb.TenantID == types.SingleTenantID {
			kb.EnsureDefaults()
			teamKBs = append(teamKBs, kb)
		}
	}
	return teamKBs, nil
}

// FillKnowledgeBaseCounts fills KnowledgeCount, ChunkCount, IsProcessing, ProcessingCount for the given KB using kb.TenantID.
func (s *knowledgeBaseService) FillKnowledgeBaseCounts(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return nil
	}
	tenantID := kb.TenantID
	kb.EnsureDefaults()
	if cnt, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
		kb.KnowledgeCount = cnt
	}
	if processingCount, err := s.kgRepo.CountKnowledgeByStatus(ctx, tenantID, kb.ID, []string{"pending", "processing"}); err == nil {
		kb.IsProcessing = processingCount > 0
		kb.ProcessingCount = processingCount
	}
	return nil
}

// UpdateKnowledgeBase updates a knowledge base's mutable properties.
func (s *knowledgeBaseService) UpdateKnowledgeBase(ctx context.Context,
	id string,
	name string,
	description string,
	config *types.KnowledgeBaseConfig,
) (*types.KnowledgeBase, error) {
	if s.nameEmbeddingRebuilder != nil && s.db != nil && !entityDatabaseInTransaction(s.db) {
		var result *types.KnowledgeBase
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			copy := *s
			copy.db = tx
			copy.repo = repository.NewKnowledgeBaseRepository(tx)
			bound, err := s.nameEmbeddingRebuilder.BindTransaction(ctx, tx)
			if err != nil {
				return err
			}
			copy.nameEmbeddingRebuilder = bound
			result, err = copy.UpdateKnowledgeBase(ctx, id, name, description, config)
			return err
		})
		return result, err
	}

	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	logger.Infof(ctx, "Updating knowledge base, ID: %s, name: %s", id, name)

	// Get existing knowledge base
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	original := *kb
	current := *kb
	kb = &current
	previousEmbeddingModelID, _ := ResolveModelTier(ctx, nil, kb, types.ModelTierEmbedding)
	// Update the knowledge base properties
	kb.Name = name
	kb.Description = description
	if config != nil {
		if config.ChunkingConfig != nil {
			if config.ChunkingConfig.ChunkSize <= 0 || config.ChunkingConfig.ChunkOverlap < 0 || config.ChunkingConfig.ChunkOverlap >= config.ChunkingConfig.ChunkSize {
				return nil, apperrors.NewBadRequestError("chunk size must be positive and chunk overlap must be smaller than chunk size")
			}
			kb.ChunkingConfig = *config.ChunkingConfig
		}
		// Update the enabled indexing strategy.
		if config.IndexingStrategy != nil {
			if !config.IndexingStrategy.HasAnyIndexing() {
				return nil, apperrors.NewBadRequestError("at least one indexing strategy must be enabled")
			}
			kb.IndexingStrategy = *config.IndexingStrategy

		}
	}
	if config != nil && config.ModelTiers != nil {
		if err := ValidateModelTierConfig(ctx, s.db, *config.ModelTiers); err != nil {
			return nil, err
		}
		kb.ModelTiers = *config.ModelTiers
		// Scalar bindings mirror the same settings for shared indexing consumers.
		kb.SummaryModelID = kb.ModelTiers.ModelID(types.ModelTierGeneration)
		kb.EmbeddingModelID = kb.ModelTiers.ModelID(types.ModelTierEmbedding)
	}
	kb.UpdatedAt = time.Now()
	kb.EnsureDefaults()

	logger.Info(ctx, "Saving knowledge base update")
	if err := s.persistKnowledgeBaseSettings(ctx, kb, &original, config); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}
	currentEmbeddingModelID, _ := ResolveModelTier(ctx, nil, kb, types.ModelTierEmbedding)
	if s.nameEmbeddingRebuilder != nil && previousEmbeddingModelID != currentEmbeddingModelID {
		if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, kb.TenantID, "knowledge_base_embedding_model_changed"); err != nil {
			return kb, fmt.Errorf("knowledge base update could not enqueue name embedding rebuild: %w", err)
		}
	}

	logger.Infof(ctx, "Knowledge base updated successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

func (s *knowledgeBaseService) ModelRoutingDB() *gorm.DB { return s.db }
