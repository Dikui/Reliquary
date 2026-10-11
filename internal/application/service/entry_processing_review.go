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

// ArticleProcessingReviewer resolves an uncertain model classification without
// hiding the already indexed article from search or its ordinary list.
type ArticleProcessingReviewer interface {
	ResolveProcessingReview(context.Context, string, string, string, bool) (*types.Entry, error)
}

func processingReviewLogValue(k *types.Entry) map[string]any {
	return map[string]any{
		"content_type":                   k.ContentType,
		"processing_mode":                k.ProcessingMode,
		"content_type_corrected":         k.ContentTypeCorrected,
		"content_type_uncertain":         k.ContentTypeUncertain,
		"processing_review_status":       k.ProcessingReviewStatus,
		"processing_review_content_type": k.ProcessingReviewContentType,
		"processing_review_reason":       k.ProcessingReviewReason,
	}
}

// ResolveProcessingReview uses the same transactional body conversion as an
// ordinary type correction. Ignoring a review changes only its status.
func (s *entryService) ResolveProcessingReview(ctx context.Context, id, action, contentType string, confirmRemoveBody bool) (*types.Entry, error) {
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated editor required")
	}
	if s.db == nil {
		return nil, errors.New("processing reviews require PostgreSQL")
	}
	if action == "confirm" || action == "correct" {
		return s.correctContentType(ctx, id, contentType, confirmRemoveBody, action)
	}
	if action != "ignore" {
		return nil, werrors.NewBadRequestError("action must be confirm, correct or ignore")
	}
	if s.vault != nil {
		lock := s.vault.articleLock(id)
		lock.Lock()
		defer lock.Unlock()
	}
	spaceID := types.MustSpaceIDFromContext(ctx)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(&current).Error; err != nil {
			return err
		}
		if current.SourceKind != "article" || current.ParseStatus != types.ParseStatusCompleted || current.ProcessingReviewStatus != types.ProcessingReviewPending {
			return werrors.NewBadRequestError("the article is not pending processing review")
		}
		before := processingReviewLogValue(&current)
		current.ProcessingReviewStatus, current.UpdatedAt = types.ProcessingReviewIgnored, time.Now()
		if err := tx.Model(&current).Updates(map[string]any{"processing_review_status": current.ProcessingReviewStatus, "updated_at": current.UpdatedAt}).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "processing_review_ignore", id, before, processingReviewLogValue(&current))
	})
	if err != nil {
		return nil, err
	}
	return s.GetEntryByID(ctx, id)
}
