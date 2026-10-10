package service

import (
	"context"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

func deleteArticleEntityAssociations(ctx context.Context, db *gorm.DB, tenantID uint64, ids []string) error {
	if db == nil || len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND knowledge_id IN ?", tenantID, ids).Delete(&types.KnowledgeEntity{}).Error; err != nil {
			return err
		}
		return tx.Where("tenant_id = ? AND knowledge_id IN ?", tenantID, ids).Delete(&types.KnowledgeTopic{}).Error
	})
}
