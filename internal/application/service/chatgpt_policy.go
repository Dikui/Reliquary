package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

type ChatGPTPolicyService struct {
	db  *gorm.DB
	now func() time.Time
}

func NewChatGPTPolicyService(db *gorm.DB) *ChatGPTPolicyService {
	return &ChatGPTPolicyService{db: db, now: time.Now}
}

func (s *ChatGPTPolicyService) locked(ctx context.Context, fn func(*types.ChatGPTPolicy) error) (*types.ChatGPTPolicy, error) {
	var p types.ChatGPTPolicy
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		initial := types.ChatGPTPolicy{ID: 1, Day: s.now().UTC().Format(time.DateOnly)}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, 1).Error; err != nil {
			return err
		}
		now := s.now().UTC()
		day := now.Format(time.DateOnly)
		if p.Day != day {
			p.Day, p.DailyUsed = day, 0
			if p.Reason == "daily_limit" {
				p.Paused, p.Reason, p.RetryAt = false, "", nil
			}
		}
		if p.Reason == "subscription_sharing_usage_unavailable" && p.RetryAt != nil && !now.Before(*p.RetryAt) {
			p.Paused, p.Reason, p.RetryAt = false, "", nil
		}
		if err := fn(&p); err != nil {
			return err
		}
		if !p.Paused && p.DailyLimit != nil && p.DailyUsed >= *p.DailyLimit {
			next := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
			p.Paused, p.Reason, p.RetryAt = true, "daily_limit", &next
		}
		p.UpdatedAt = now
		return tx.Save(&p).Error
	})
	return &p, err
}

func (s *ChatGPTPolicyService) Get(ctx context.Context) (*types.ChatGPTPolicy, error) {
	if s.db.Migrator().HasTable("maintenance_state") {
		paused, err := maintenanceops.SnapshotPaused(ctx, s.db)
		if err != nil {
			return nil, err
		}
		if paused {
			var p types.ChatGPTPolicy
			err := s.db.WithContext(ctx).First(&p, 1).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &types.ChatGPTPolicy{ID: 1, Day: s.now().UTC().Format(time.DateOnly)}, nil
			}
			return &p, err
		}
	}
	return s.locked(ctx, func(*types.ChatGPTPolicy) error { return nil })
}

func (s *ChatGPTPolicyService) Update(ctx context.Context, limit *int, fallback string) (*types.ChatGPTPolicy, error) {
	if limit != nil && *limit <= 0 {
		return nil, errors.New("daily_limit must be a positive integer or null")
	}
	if fallback != "" {
		var model types.Model
		if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", fallback, types.SingleSpaceID).First(&model).Error; err != nil {
			return nil, fmt.Errorf("fallback model: %w", err)
		}
		if model.Source != types.ModelSourceLocal || model.Type != types.ModelTypeKnowledgeQA || model.Status != types.ModelStatusActive {
			return nil, errors.New("fallback must be an active local generation model")
		}
	}
	return s.locked(ctx, func(p *types.ChatGPTPolicy) error {
		p.DailyLimit, p.FallbackModelID = limit, fallback
		if p.Reason == "daily_limit" && (limit == nil || p.DailyUsed < *limit) {
			p.Paused, p.Reason, p.RetryAt = false, "", nil
		}
		return nil
	})
}

func (s *ChatGPTPolicyService) Reserve(ctx context.Context) (*types.ChatGPTPolicy, error) {
	allowed := false
	p, err := s.locked(ctx, func(p *types.ChatGPTPolicy) error {
		if p.Paused {
			return nil
		}
		if p.DailyLimit != nil && p.DailyUsed >= *p.DailyLimit {
			next := s.now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			p.Paused, p.Reason, p.RetryAt = true, "daily_limit", &next
			return nil
		}
		p.DailyUsed++
		allowed = true
		return nil
	})
	if err == nil && !allowed {
		return p, s.deferred(p, nil)
	}
	return p, err
}

func (s *ChatGPTPolicyService) Pause(ctx context.Context, cause error) (*types.ChatGPTPolicy, error) {
	var upstream *types.ChatGPTError
	if !errors.As(cause, &upstream) || !types.IsChatGPTQuotaError(cause) {
		return nil, cause
	}
	return s.locked(ctx, func(p *types.ChatGPTPolicy) error {
		p.Paused, p.Reason, p.RetryAt = true, upstream.Code, nil
		if upstream.Code == "subscription_sharing_usage_unavailable" {
			next := s.now().UTC().Add(5 * time.Minute)
			p.RetryAt = &next
		}
		return nil
	})
}

func (s *ChatGPTPolicyService) Resume(ctx context.Context) (*types.ChatGPTPolicy, error) {
	return s.locked(ctx, func(p *types.ChatGPTPolicy) error {
		if p.DailyLimit != nil && p.DailyUsed >= *p.DailyLimit {
			next := s.now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
			p.Paused, p.Reason, p.RetryAt = true, "daily_limit", &next
		} else {
			p.Paused, p.Reason, p.RetryAt = false, "", nil
		}
		return nil
	})
}

func (s *ChatGPTPolicyService) deferred(p *types.ChatGPTPolicy, cause error) error {
	retry := s.now().UTC().Add(5 * time.Minute)
	if p.RetryAt != nil {
		retry = *p.RetryAt
	}
	return &types.ChatGPTDeferredError{Reason: p.Reason, RetryAt: retry, Cause: cause}
}
