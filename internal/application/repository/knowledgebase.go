package repository

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var ErrKnowledgeBaseNotFound = errors.New("knowledge base not found")

// knowledgeBaseRepository implements the KnowledgeBaseRepository interface
type knowledgeBaseRepository struct {
	db *gorm.DB
}

// NewKnowledgeBaseRepository creates a new knowledge base repository
func NewKnowledgeBaseRepository(db *gorm.DB) interfaces.KnowledgeBaseRepository {
	return &knowledgeBaseRepository{db: db}
}

// CreateKnowledgeBase creates a new knowledge base
func (r *knowledgeBaseRepository) CreateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
	return r.db.WithContext(ctx).Create(kb).Error
}

// GetKnowledgeBaseByID gets a knowledge base by id within the personal space
func (r *knowledgeBaseRepository) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ? AND space_id = ?", id, types.SingleSpaceID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// GetKnowledgeBaseByIDAndSpace gets a knowledge base by id only if it belongs to the given space (enforces space isolation)
func (r *knowledgeBaseRepository) GetKnowledgeBaseByIDAndSpace(ctx context.Context, id string, spaceID uint64) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id = ? AND space_id = ?", id, spaceID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// GetKnowledgeBaseByIDs gets knowledge bases by multiple ids
func (r *knowledgeBaseRepository) GetKnowledgeBaseByIDs(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	if len(ids) == 0 {
		return []*types.KnowledgeBase{}, nil
	}
	var kbs []*types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("id IN ? AND space_id = ?", ids, types.SingleSpaceID).Find(&kbs).Error; err != nil {
		return nil, err
	}
	return kbs, nil
}

// GetKnowledgeBaseBySpaceID returns the active knowledge base for one space.
func (r *knowledgeBaseRepository) GetKnowledgeBaseBySpaceID(ctx context.Context, spaceID uint64) (*types.KnowledgeBase, error) {
	var kb types.KnowledgeBase
	if err := r.db.WithContext(ctx).Where("space_id = ?", spaceID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrKnowledgeBaseNotFound
		}
		return nil, err
	}
	return &kb, nil
}

// UpdateKnowledgeBase updates a knowledge base
func (r *knowledgeBaseRepository) UpdateKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase) error {
	return r.db.WithContext(ctx).Save(kb).Error
}

// CountByModelID counts active knowledge bases that reference modelID in any
// model-binding column (scalar fields or JSON config blobs).
func (r *knowledgeBaseRepository) CountByModelID(
	ctx context.Context, spaceID uint64, modelID string,
) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).
		Model(&types.KnowledgeBase{}).
		Where("space_id = ?", spaceID)
	query = scopeKnowledgeBasesByModelID(query, modelID)
	err := query.Count(&count).Error
	return count, err
}

// ListModelUsages returns active knowledge bases that reference modelID. It
// selects only the columns needed to identify the resource and its bindings;
// GORM's default scope excludes soft-deleted rows.
func (r *knowledgeBaseRepository) ListModelUsages(
	ctx context.Context, spaceID uint64, modelID string,
) ([]types.ModelUsageResource, error) {
	rows := make([]*types.KnowledgeBase, 0)
	query := r.db.WithContext(ctx).
		Model(&types.KnowledgeBase{}).
		Select(
			"id", "name", "embedding_model_id", "summary_model_id",
			"model_tiers",
		).
		Where("space_id = ?", spaceID)
	query = scopeKnowledgeBasesByModelID(query, modelID)
	if err := query.Order("name ASC, id ASC").Limit(types.ModelUsageListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}

	usages := make([]types.ModelUsageResource, 0, len(rows))
	for _, row := range rows {
		bindings := knowledgeBaseModelUsageBindings(row, modelID)
		if len(bindings) == 0 {
			continue
		}
		usages = append(usages, types.ModelUsageResource{
			ID:       row.ID,
			Name:     row.Name,
			Bindings: bindings,
		})
	}
	return usages, nil
}

// EnsureSpaceKnowledgeBase creates the settings resource once. Callers may pass
// an existing registration transaction so the user and knowledge base commit together.
func EnsureSpaceKnowledgeBase(ctx context.Context, db *gorm.DB, spaceID uint64, creatorID string) (*types.KnowledgeBase, error) {
	if spaceID != types.SingleSpaceID {
		return nil, errors.New("invalid personal space")
	}
	var kb *types.KnowledgeBase
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651002)).Error; err != nil {
			return err
		}
		repo := NewKnowledgeBaseRepository(tx)
		existing, err := repo.GetKnowledgeBaseBySpaceID(ctx, spaceID)
		if err == nil {
			kb = existing
			return nil
		}
		if !errors.Is(err, ErrKnowledgeBaseNotFound) {
			return err
		}
		name := "Knowledge Base"
		if strings.HasPrefix(strings.ToLower(types.LanguageFromContextOrDefault(ctx)), "zh") {
			name = "知识库"
		}
		now := time.Now().UTC()
		kb = &types.KnowledgeBase{ID: uuid.NewString(), SpaceID: spaceID, CreatorID: creatorID, Name: name, Type: types.KnowledgeBaseTypeDocument, CreatedAt: now, UpdatedAt: now}
		kb.EnsureDefaults()
		return repo.CreateKnowledgeBase(ctx, kb)
	})
	return kb, err
}
