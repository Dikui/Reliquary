package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func closeMergedEntitySuggestions(tx *gorm.DB, spaceID uint64, keepID, mergedID string) error {
	// Only the chosen pair was accepted. Other suggestions for the absorbed
	// identity are obsolete; suggestions for the retained identity remain usable.
	return tx.Model(&types.EntityMergeSuggestion{}).
		Where("space_id = ? AND status = 'pending'", spaceID).
		Where("new_entity_id = ? OR candidate_entity_id = ?", mergedID, mergedID).
		Updates(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN new_entity_id = ? OR candidate_entity_id = ? THEN 'accepted' ELSE 'rejected' END",
				keepID, keepID),
			"decided_at": time.Now(),
		}).Error
}

func (s *EntityService) ListMergeSuggestions(ctx context.Context, q types.EntityQuery, status string) ([]types.EntityMergeSuggestion, error) {
	spaceID, libraryID, err := s.resolveLibrary(ctx, q.LibraryID)
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "accepted" && status != "rejected" && status != "all" {
		return nil, werrors.NewBadRequestError("status must be pending, accepted, rejected or all")
	}
	items := []types.EntityMergeSuggestion{}
	query := s.db.WithContext(ctx).Model(&types.EntityMergeSuggestion{}).
		Select(`entity_merge_suggestions.*, fresh.canonical_name AS new_entity_name, candidate.canonical_name AS candidate_entity_name,
            (SELECT record.keep_entity_id FROM entity_merge_records record
             WHERE record.space_id=entity_merge_suggestions.space_id
               AND ((record.keep_entity_id=fresh.id AND record.merge_entity_id=candidate.id)
                 OR (record.keep_entity_id=candidate.id AND record.merge_entity_id=fresh.id))
             ORDER BY record.created_at DESC LIMIT 1) AS keep_entity_id`).
		Joins("JOIN entities fresh ON fresh.space_id=entity_merge_suggestions.space_id AND fresh.id=entity_merge_suggestions.new_entity_id").
		Joins("JOIN entities candidate ON candidate.space_id=entity_merge_suggestions.space_id AND candidate.id=entity_merge_suggestions.candidate_entity_id").
		Where("entity_merge_suggestions.space_id=?", spaceID)
	query = query.Where("fresh.library_id=? AND candidate.library_id=?", libraryID, libraryID)
	if status != "all" {
		query = query.Where("entity_merge_suggestions.status=?", status)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	err = query.Order("entity_merge_suggestions.created_at DESC,entity_merge_suggestions.id").Limit(limit).Offset(q.Offset).Scan(&items).Error
	return items, err
}

func (s *EntityService) DecideMergeSuggestion(ctx context.Context, suggestionID string, accept bool, keepEntityID string) (*types.EntityMergeSuggestion, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated merge suggestion editor required")
	}
	var item types.EntityMergeSuggestion
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", suggestionID, spaceID).First(&item).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("merge suggestion not found")
		} else if err != nil {
			return err
		}

		target := "rejected"
		mergeEntityID := item.NewEntityID
		if accept {
			target = "accepted"
			if keepEntityID == "" {
				keepEntityID = item.CandidateEntityID
			}
			if keepEntityID != item.NewEntityID && keepEntityID != item.CandidateEntityID {
				return werrors.NewBadRequestError("retained entity must be one of the suggestion candidates")
			}
			if keepEntityID == item.NewEntityID {
				mergeEntityID = item.CandidateEntityID
			}
			item.KeepEntityID = keepEntityID
		}
		if item.Status == target {
			if accept {
				var count int64
				if err := tx.Model(&types.EntityMergeRecord{}).Where("space_id=? AND keep_entity_id=? AND merge_entity_id=?", spaceID, keepEntityID, mergeEntityID).Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					return werrors.NewBadRequestError("merge suggestion was accepted with a different retained entity")
				}
			}
			return nil
		}
		if item.Status != "pending" {
			return werrors.NewBadRequestError("merge suggestion has already been decided")
		}
		if accept {
			nested := *s
			nested.db = tx
			if err := nested.MergeEntities(ctx, keepEntityID, mergeEntityID); err != nil {
				return err
			}
		}
		now := time.Now()
		item.Status = target
		item.DecidedAt = &now
		return tx.Model(&item).Updates(map[string]any{"status": target, "decided_at": now}).Error
	})
	if err == nil && accept {
		err = s.refreshArticleHeadersForEntity(ctx, spaceID, keepEntityID)
	}
	return &item, err
}
