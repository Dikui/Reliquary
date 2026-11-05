package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func equalManagedAttributes(a, b map[string]any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

type EntityAttributesResult struct {
	EntityID    string         `json:"entity_id"`
	At          time.Time      `json:"at"`
	Known       bool           `json:"known"`
	Attributes  map[string]any `json:"attributes"`
	Revision    int64          `json:"revision,omitempty"`
	EffectiveAt *time.Time     `json:"effective_at,omitempty"`
}

type EntityAttributeHistoryPage struct {
	Items    []types.EntityAttributeVersion `json:"items"`
	Total    int64                          `json:"total"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"page_size"`
}

// Resolve current values at read time; future snapshots never leak into current attributes.
func hydrateManagedEntityAttributes(ctx context.Context, db *gorm.DB, entities []*types.Entity, at time.Time) error {
	ids := make([]string, 0, len(entities))
	for _, entity := range entities {
		ids = append(ids, entity.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var managed []string
	if err := db.WithContext(ctx).Table("entities e").Joins("JOIN libraries l ON l.id=e.library_id AND l.space_id=e.space_id").Where("e.id IN ? AND l.entity_mode=?", ids, types.EntityModeManaged).Pluck("e.id", &managed).Error; err != nil {
		return err
	}
	if len(managed) == 0 {
		return nil
	}
	var versions []types.EntityAttributeVersion
	if err := db.WithContext(ctx).Select("DISTINCT ON (entity_id) *").Where("entity_id IN ? AND effective_at<=?", managed, at).Order("entity_id,effective_at DESC,revision DESC").Find(&versions).Error; err != nil {
		return err
	}
	attrs := map[string]map[string]any{}
	for _, id := range managed {
		attrs[id] = map[string]any{}
	}
	for _, version := range versions {
		attrs[version.EntityID] = version.Attributes
	}
	for _, entity := range entities {
		if attributes, ok := attrs[entity.ID]; ok {
			entity.Attributes = attributes
		}
	}
	return nil
}

func saveManagedAttributeVersion(ctx context.Context, tx *gorm.DB, entity *types.Entity, effectiveAt *time.Time) error {
	now := time.Now()
	effective := now
	if effectiveAt != nil {
		effective = *effectiveAt
	}
	if effective.IsZero() {
		return apperrors.NewBadRequestError("effective_at must be a valid date")
	}
	var previous types.EntityAttributeVersion
	err := tx.Where("entity_id=? AND effective_at<=?", entity.ID, effective).Order("effective_at DESC,revision DESC").First(&previous).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil && equalManagedAttributes(previous.Attributes, entity.Attributes) {
		if err := hydrateManagedEntityAttributes(ctx, tx, []*types.Entity{entity}, now); err != nil {
			return err
		}
		return tx.Model(entity).Select("attributes").Updates(entity).Error
	}
	// Carry this edit into pending snapshots without replacing their other fields.
	var pending []types.EntityAttributeVersion
	if err := tx.Where("entity_id=? AND effective_at>? AND effective_at>?", entity.ID, now, effective).Find(&pending).Error; err != nil {
		return err
	}
	for _, next := range pending {
		before := next
		updated := make(map[string]any, len(next.Attributes))
		for key, value := range next.Attributes {
			updated[key] = value
		}
		for key, value := range entity.Attributes {
			old, exists := previous.Attributes[key]
			if !exists || !equalManagedAttributes(map[string]any{key: old}, map[string]any{key: value}) {
				updated[key] = value
			}
		}
		for key := range previous.Attributes {
			if _, exists := entity.Attributes[key]; !exists {
				delete(updated, key)
			}
		}
		if equalManagedAttributes(next.Attributes, updated) {
			continue
		}
		next.Attributes = updated
		if err := tx.Model(&next).Select("attributes").Updates(&next).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "update_pending_entity_attributes", entity.ID, before, next); err != nil {
			return err
		}
	}
	version := types.EntityAttributeVersion{ID: uuid.NewString(), SpaceID: entity.SpaceID, EntityID: entity.ID, Revision: entity.Revision, EffectiveAt: effective, RecordedAt: now, Attributes: entity.Attributes}
	if err := tx.Create(&version).Error; err != nil {
		return err
	}
	if err := hydrateManagedEntityAttributes(ctx, tx, []*types.Entity{entity}, now); err != nil {
		return err
	}
	return tx.Model(entity).Select("attributes").Updates(entity).Error
}

func (s *EntityService) managedEntityForRead(ctx context.Context, id string) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, spaceID).First(&entity).Error; err != nil {
		return nil, managedWriteError(err)
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", entity.LibraryID, spaceID).First(&library).Error; err != nil {
		return nil, err
	}
	if !library.ManagesEntities() {
		return nil, apperrors.NewBadRequestError("attribute history requires a managed entity")
	}
	return &entity, nil
}

func (s *EntityService) EntityAttributes(ctx context.Context, id string, at time.Time) (*EntityAttributesResult, error) {
	entity, err := s.managedEntityForRead(ctx, id)
	if err != nil {
		return nil, err
	}
	result := &EntityAttributesResult{EntityID: id, At: at, Attributes: map[string]any{}}
	var version types.EntityAttributeVersion
	err = s.db.WithContext(ctx).Where("space_id=? AND entity_id=? AND effective_at<=?", entity.SpaceID, id, at).Order("effective_at DESC,revision DESC").First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Known, result.Attributes, result.Revision, result.EffectiveAt = true, version.Attributes, version.Revision, &version.EffectiveAt
	return result, nil
}

func (s *EntityService) EntityAttributeHistory(ctx context.Context, id string, page, pageSize int) (*EntityAttributeHistoryPage, error) {
	entity, err := s.managedEntityForRead(ctx, id)
	if err != nil {
		return nil, err
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, apperrors.NewBadRequestError("invalid history page")
	}
	result := &EntityAttributeHistoryPage{Items: []types.EntityAttributeVersion{}, Page: page, PageSize: pageSize}
	query := s.db.WithContext(ctx).Model(&types.EntityAttributeVersion{}).Where("space_id=? AND entity_id=?", entity.SpaceID, id)
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	err = query.Order("effective_at DESC,revision DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&result.Items).Error
	return result, err
}

func (s *EntityService) EntityReorganizations(ctx context.Context, id string) ([]types.EntityReorganization, error) {
	entity, err := s.managedEntityForRead(ctx, id)
	if err != nil {
		return nil, err
	}
	result := []types.EntityReorganization{}
	err = s.db.WithContext(ctx).Where("space_id=? AND (source_id=? OR target_id=?)", entity.SpaceID, id, id).Order("created_at DESC,id").Find(&result).Error
	return result, err
}
