package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

// EnsureTenantKnowledgeBase creates the settings resource once. Callers may pass
// an existing registration transaction so the user and knowledge base commit together.
func EnsureTenantKnowledgeBase(ctx context.Context, db *gorm.DB, tenantID uint64, creatorID string) (*types.KnowledgeBase, error) {
	if tenantID != types.SingleTenantID {
		return nil, errors.New("invalid personal space")
	}
	var kb *types.KnowledgeBase
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651002)).Error; err != nil {
			return err
		}
		repo := NewKnowledgeBaseRepository(tx)
		existing, err := repo.GetKnowledgeBaseByTenantID(ctx, tenantID)
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
		kb = &types.KnowledgeBase{ID: uuid.NewString(), TenantID: tenantID, CreatorID: creatorID, Name: name, Type: types.KnowledgeBaseTypeDocument, CreatedAt: now, UpdatedAt: now}
		kb.EnsureDefaults()
		return repo.CreateKnowledgeBase(ctx, kb)
	})
	return kb, err
}
