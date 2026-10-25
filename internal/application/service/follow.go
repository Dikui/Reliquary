package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"strings"

	werrors "reliquary/internal/errors"

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
	if targetType != types.FollowTargetEntity && targetType != types.FollowTargetStory {
		return werrors.NewBadRequestError("target_type must be entity or story")
	}
	if strings.TrimSpace(targetID) == "" || len(targetID) > 36 {
		return werrors.NewBadRequestError("a target_id of at most 36 characters is required")
	}
	return nil
}

// resolveFollowTarget locks the object against a concurrent merge or deletion.
func resolveFollowTarget(tx *gorm.DB, spaceID uint64, targetType, targetID string) (*types.FollowTarget, error) {
	if targetType == types.FollowTargetStory {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return nil, err
		}
		story, err := canonicalStory(tx, spaceID, targetID)
		if err != nil {
			return nil, err
		}
		return &types.FollowTarget{TargetType: targetType, TargetID: story.ID, Name: story.Title}, nil
	}
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

	return nil, werrors.NewBadRequestError("target_type must be entity or story")
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

func (s *FollowService) ListFollows(ctx context.Context) ([]types.FollowTarget, error) {
	spaceID, err := followSpace(ctx)
	if err != nil {
		return nil, err
	}
	result := []types.FollowTarget{}
	err = s.followTargets(ctx, spaceID).Scan(&result).Error
	return result, err
}

func (s *FollowService) followTargets(ctx context.Context, spaceID uint64) *gorm.DB {
	return s.db.WithContext(ctx).Table("follows follow").
		Select("follow.target_type,follow.target_id,follow.created_at,COALESCE(e.canonical_name,st.title) AS name").
		Joins(`LEFT JOIN entities e ON follow.target_type='entity'
 AND e.space_id=follow.space_id AND e.id=follow.target_id`).
		Joins(`LEFT JOIN stories st ON follow.target_type='story'
 AND st.space_id=follow.space_id AND st.id=follow.target_id`).
		Where(`follow.space_id=? AND ((follow.target_type='entity' AND e.merged_into_id='')
 OR (follow.target_type='story' AND st.merged_into_id=''))`, spaceID).
		Order("name,follow.target_type,follow.target_id")
}
