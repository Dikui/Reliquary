package types

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RetireEmbeddingVectors runs in the transaction that changes model semantics.
// Vector bytes remain stored, but the active model ID cannot retrieve them.
func RetireEmbeddingVectors(ctx context.Context, tx *gorm.DB, modelID string) error {
	stale := "stale:" + uuid.NewString()
	if err := tx.WithContext(ctx).Exec(`UPDATE embeddings SET model_id=?,updated_at=NOW() WHERE model_id=?
		OR (model_id='' AND entry_id IN (SELECT id FROM entries WHERE embedding_model_id=?))`, stale, modelID, modelID).Error; err != nil {
		return err
	}
	for _, table := range []string{"article_embeddings", "image_embeddings"} {
		if err := tx.WithContext(ctx).Table(table).Where("model_id=?", modelID).Updates(map[string]any{"model_id": stale, "updated_at": gorm.Expr("NOW()")}).Error; err != nil {
			return err
		}
	}
	return nil
}
