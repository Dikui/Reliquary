package service

import (
	"context"
	"fmt"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"

	"gorm.io/gorm"
)

// EnsurePersonalSpace runs before any request or background task starts.
func EnsurePersonalSpace(ctx context.Context, db *gorm.DB) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureDefaultLocalEmbedding(ctx, tx); err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(72651000)).Error; err != nil {
			return err
		}
		repo := repository.NewSpaceRepository(tx)
		spaces, err := repo.ListSpaces(ctx)
		if err != nil {
			return err
		}
		if len(spaces) == 1 && spaces[0].ID == types.SingleSpaceID {
			return nil
		}
		if len(spaces) != 0 {
			return fmt.Errorf("expected only personal workspace %d, found %d workspaces", types.SingleSpaceID, len(spaces))
		}
		svc := NewSpaceService(repo)
		_, err = svc.CreateSpace(ctx, &types.Space{
			ID: types.SingleSpaceID, EmbeddingModelID: "builtin-qwen3-embedding-06b", Name: "Personal workspace", Description: "Personal entry", Status: "active",
		})
		return err
	})
	if err != nil {
		return err
	}
	return backfillBodyLocations(ctx, db)
}

func ensureDefaultLocalEmbedding(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec(`INSERT INTO models(id,space_id,name,type,source,description,parameters,is_default,status,is_builtin)
 VALUES ('builtin-qwen3-embedding-06b',1,'qwen3-embedding:0.6b','Embedding','local','Local text embedding',
 '{"embedding_parameters":{"dimension":1024,"truncate_prompt_tokens":8192},"base_url":"http://127.0.0.1:11434"}',false,'active',true) ON CONFLICT(id) DO NOTHING`).Error
}
