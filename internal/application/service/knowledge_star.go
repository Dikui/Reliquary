package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *knowledgeService) SetStarred(ctx context.Context, id string, starred bool) (*types.Knowledge, error) {
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok {
		return nil, werrors.NewUnauthorizedError("personal space is required")
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated editor required")
	}
	var article types.Knowledge
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, id).First(&article).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("article not found")
			}
			return err
		}
		if article.SourceKind != "article" {
			return werrors.NewBadRequestError("only articles can be starred")
		}
		if article.Starred == starred {
			return nil
		}
		before := article.Starred
		if err := tx.Model(&article).Updates(map[string]any{"starred": starred, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		action := "star"
		if !starred {
			action = "unstar"
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, action, id, map[string]any{"starred": before}, map[string]any{"starred": starred})
	})
	if err != nil {
		return nil, err
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	return s.GetKnowledgeByID(ctx, id)
}
