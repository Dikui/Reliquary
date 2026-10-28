package service

import (
	"context"
	"errors"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DomainService owns the shared direction list and manual material choices.
type DomainService struct{ db *gorm.DB }

// NewDomainService constructs the shared domain directory.
func NewDomainService(db *gorm.DB) *DomainService { return &DomainService{db: db} }

func domainSpace(ctx context.Context) (uint64, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

func lockDomainSpace(tx *gorm.DB, spaceID uint64) error {
	return tx.Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
		"reliquary:domains:"+strconv.FormatUint(spaceID, 10),
	).Error
}

// ListDomains returns every maintained domain in name order.
func (s *DomainService) ListDomains(ctx context.Context) ([]types.Domain, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	items := []types.Domain{}
	err = s.db.WithContext(ctx).Where("space_id=?", spaceID).Order("name,id").Find(&items).Error
	return items, err
}

// SaveDomain creates or replaces a domain name and definition.
func (s *DomainService) SaveDomain(ctx context.Context, id string, input types.DomainInput) (*types.Domain, error) {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return nil, err
	}
	input.Name, input.Definition = strings.TrimSpace(input.Name), strings.TrimSpace(input.Definition)
	key := types.NormalizeEntityKey(input.Name)
	if key == "" || input.Definition == "" || utf8.RuneCountInString(input.Name) > 128 ||
		utf8.RuneCountInString(key) > 128 ||
		utf8.RuneCountInString(input.Definition) > 2000 {
		return nil, werrors.NewBadRequestError(
			"domain name (1-128 characters) and definition (1-2000 characters) required",
		)
	}
	var item types.Domain
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDomainSpace(tx, spaceID); err != nil {
			return err
		}
		if id != "" {
			if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&item).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return werrors.NewNotFoundError("domain not found")
				}
				return err
			}
		}
		var duplicates int64
		if err := tx.Model(&types.Domain{}).
			Where("space_id=? AND normalized_name=? AND id<>?", spaceID, key, id).Count(&duplicates).Error; err != nil {
			return err
		}
		if duplicates > 0 {
			return werrors.NewConflictError("domain name already exists")
		}
		if id == "" {
			item.ID, item.SpaceID, item.CreatedAt = uuid.NewString(), spaceID, time.Now()
		}
		item.Name, item.NormalizedName, item.Definition, item.UpdatedAt = input.Name, key, input.Definition, time.Now()
		return tx.Save(&item).Error
	})
	return &item, err
}

// DeleteDomain removes a domain and its material associations atomically.
func (s *DomainService) DeleteDomain(ctx context.Context, id string) error {
	spaceID, err := domainSpace(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDomainSpace(tx, spaceID); err != nil {
			return err
		}
		result := tx.Where("space_id=? AND id=?", spaceID, id).Delete(&types.Domain{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return werrors.NewNotFoundError("domain not found")
		}
		return nil
	})
}
