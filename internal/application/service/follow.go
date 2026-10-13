package service

import (
	"context"
	"errors"
	"strings"
	"time"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FollowService struct {
	db   *gorm.DB
	logs *OperationLogService
}

func NewFollowService(db *gorm.DB) *FollowService {
	return &FollowService{db: db, logs: NewOperationLogService(db)}
}

func (s *FollowService) SetOperationLogs(logs *OperationLogService) { s.logs = logs }

func followSpace(ctx context.Context) (uint64, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

func validateFollowTarget(targetType, targetID string) error {
	if targetType != types.FollowTargetEntity && targetType != types.FollowTargetTopic {
		return werrors.NewBadRequestError("target_type must be entity or topic")
	}
	if strings.TrimSpace(targetID) == "" || len(targetID) > 36 {
		return werrors.NewBadRequestError("a target_id of at most 36 characters is required")
	}
	return nil
}

// resolveFollowTarget locks the object against a concurrent merge or deletion.
func resolveFollowTarget(tx *gorm.DB, spaceID uint64, targetType, targetID string) (*types.FollowTarget, error) {
	if targetType == types.FollowTargetEntity {
		if err := lockEntitySpaceShared(tx, spaceID); err != nil {
			return nil, err
		}
		visited := map[string]bool{}
		for !visited[targetID] {
			visited[targetID] = true
			var entity types.Entity
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, targetID).First(&entity).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, werrors.NewNotFoundError("entity not found")
				}
				return nil, err
			}
			if entity.MergedIntoID == "" {
				return &types.FollowTarget{TargetType: targetType, TargetID: entity.ID, Name: entity.CanonicalName}, nil
			}
			targetID = entity.MergedIntoID
		}
		return nil, werrors.NewBadRequestError("entity merge cycle")
	}
	var topic types.Topic
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", spaceID, targetID).First(&topic).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("topic not found")
		}
		return nil, err
	}
	return &types.FollowTarget{TargetType: targetType, TargetID: topic.ID, Name: topic.Name}, nil
}

func (s *FollowService) Follow(ctx context.Context, targetType, targetID string) (*types.FollowTarget, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateFollowTarget(targetType, targetID); err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated follow actor required")
	}
	var target *types.FollowTarget
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		target, err = resolveFollowTarget(tx, spaceID, targetType, targetID)
		if err != nil {
			return err
		}
		row := types.Follow{SpaceID: spaceID, TargetType: targetType, TargetID: target.TargetID}
		if err := tx.Where("space_id=? AND target_type=? AND target_id=?", spaceID, targetType, target.TargetID).First(&row).Error; err == nil {
			target.CreatedAt = row.CreatedAt
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		target.CreatedAt = row.CreatedAt
		return s.logs.RecordTx(ctx, tx, "follow", target.TargetID, nil, target)
	})
	if err != nil {
		return nil, err
	}
	items, err := s.ListFollows(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.TargetType == targetType && item.TargetID == target.TargetID {
			return &item, nil
		}
	}
	return target, nil
}

func (s *FollowService) Unfollow(ctx context.Context, targetType, targetID string) error {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return err
	}
	if err := validateFollowTarget(targetType, targetID); err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return werrors.NewUnauthorizedError("authenticated follow actor required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		target, err := resolveFollowTarget(tx, spaceID, targetType, targetID)
		if err != nil {
			var appErr *werrors.AppError
			if !errors.As(err, &appErr) || appErr.Code != werrors.ErrNotFound {
				return err
			}
			// A previously deleted object is already unfollowed by its foreign key.
			var count int64
			if readErr := tx.Model(&types.Follow{}).Where("space_id=? AND target_type=? AND target_id=?", spaceID, targetType, targetID).Count(&count).Error; readErr != nil {
				return readErr
			}
			if count == 0 {
				return nil
			}
			return err
		}
		var row types.Follow
		if err := tx.Where("space_id=? AND target_type=? AND target_id=?", spaceID, targetType, target.TargetID).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		target.CreatedAt = row.CreatedAt
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		return s.logs.RecordTx(ctx, tx, "unfollow", target.TargetID, target, nil)
	})
}

const followEligibleSQL = `k.deleted_at IS NULL AND k.source_kind='article'
    AND k.parse_status='completed' AND k.enable_status='enabled'
    AND COALESCE(k.metadata #>> '{intake,decision}','approved')='approved'`

const followMatchSQL = `((f.target_type='entity' AND EXISTS (SELECT 1 FROM entry_entities ke
    WHERE ke.space_id=k.space_id AND ke.entry_id=k.id AND ke.entity_id=f.target_id))
    OR (f.target_type='topic' AND EXISTS (SELECT 1 FROM entry_topics kt
    WHERE kt.space_id=k.space_id AND kt.entry_id=k.id AND kt.topic_id=f.target_id)))`

func (s *FollowService) ListFollows(ctx context.Context) ([]types.FollowTarget, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC()
	result := []types.FollowTarget{}
	err = s.db.WithContext(ctx).Raw(`SELECT f.target_type,f.target_id,f.created_at,
        CASE WHEN f.target_type='entity' THEN e.canonical_name ELSE t.name END AS name,
        (SELECT count(*) FROM entries k WHERE k.space_id=f.space_id AND `+followEligibleSQL+`
            AND k.created_at>=? AND k.created_at<=? AND `+followMatchSQL+`) AS recent_article_count
        FROM follows f LEFT JOIN entities e ON f.target_type='entity' AND e.space_id=f.space_id AND e.id=f.target_id
        LEFT JOIN topics t ON f.target_type='topic' AND t.space_id=f.space_id AND t.id=f.target_id
        WHERE f.space_id=? AND ((f.target_type='entity' AND e.merged_into_id='') OR (f.target_type='topic' AND t.id IS NOT NULL))
        ORDER BY name,f.target_type,f.target_id`, cutoff.AddDate(0, 0, -7), cutoff, spaceID).Scan(&result).Error
	return result, err
}
