package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func managedLibrary(tx *gorm.DB, spaceID uint64, id string) (*types.Library, error) {
	var library types.Library
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("RelationTypes").Where("space_id=? AND id=?", spaceID, id).First(&library).Error; err != nil {
		return nil, managedWriteError(err)
	}
	if !library.ManagesEntities() {
		return nil, apperrors.NewBadRequestError("this operation requires a managed entity library")
	}
	return &library, nil
}

func managedWriteError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewNotFoundError("entity, relationship or library not found")
	}
	type sqlState interface{ SQLState() string }
	var state sqlState
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return apperrors.NewConflictError("name, code, alias or relationship already exists")
	}
	return err
}

func validateManagedEntity(library *types.Library, entity *types.Entity) error {
	entity.CanonicalName = strings.TrimSpace(entity.CanonicalName)
	entity.Code = strings.TrimSpace(entity.Code)
	entity.Description = strings.TrimSpace(entity.Description)
	if entity.CanonicalName == "" || len([]rune(entity.CanonicalName)) > 512 || len([]rune(entity.Code)) > 128 || len([]rune(entity.Description)) > 20000 {
		return apperrors.NewBadRequestError("name is required; name, code or description is too long")
	}
	entity.NormalizedName = normalizeEntityName(entity.CanonicalName)
	if entity.Attributes == nil {
		entity.Attributes = map[string]any{}
	}
	if err := library.EntitySchema.ValidateAttributes(entity.Kind, entity.Attributes); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	return nil
}

func saveManagedAliases(tx *gorm.DB, entity *types.Entity, aliases []string) error {
	if len(aliases) > 100 {
		return apperrors.NewBadRequestError("at most 100 aliases are allowed")
	}
	if err := tx.Where("space_id=? AND entity_id=?", entity.SpaceID, entity.ID).Delete(&types.EntityAlias{}).Error; err != nil {
		return err
	}
	entity.Aliases = []string{}
	seen := map[string]bool{}
	for _, alias := range append(aliases, entity.CanonicalName) {
		alias = strings.TrimSpace(alias)
		if alias == "" || len([]rune(alias)) > 512 {
			return apperrors.NewBadRequestError("aliases must contain 1-512 characters")
		}
		key := normalizeEntityName(alias)
		if seen[key] {
			continue
		}
		seen[key] = true
		var count int64
		if err := tx.Model(&types.Entity{}).Where("space_id=? AND library_id=? AND entity_type=? AND normalized_name=? AND id<>? AND merged_into_id=''", entity.SpaceID, entity.LibraryID, entity.EntityType, key, entity.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewConflictError("alias conflicts with another entity name")
		}
		if err := saveEntityAlias(tx, *entity, alias, "manual"); err != nil {
			return err
		}
		entity.Aliases = append(entity.Aliases, alias)
	}
	return nil
}

func queueManagedEntity(tx *gorm.DB, entity *types.Entity) error {
	if err := tx.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: entity.SpaceID, EntityID: entity.ID, RelationVersion: entity.Revision, Deleted: entity.ArchivedAt != nil, CreatedAt: time.Now()}).Error; err != nil {
		return err
	}
	var relations []types.Relation
	if err := tx.Where("space_id=? AND (subject_id=? OR object_id=?)", entity.SpaceID, entity.ID, entity.ID).Find(&relations).Error; err != nil {
		return err
	}
	for _, r := range relations {
		peer := r.SubjectID
		if peer == entity.ID {
			peer = r.ObjectID
		}
		if err := notifyEntityProjection(tx, entity.SpaceID, peer); err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityService) CreateManagedEntity(ctx context.Context, libraryID string, input types.ManagedEntityInput) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("entity editor required")
	}
	entity := types.Entity{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: libraryID, EntityType: "product", Kind: input.Kind, Code: input.Code, CanonicalName: input.Name, Description: input.Description, Attributes: input.Attributes, Revision: 1, EmbeddingNeedsRebuild: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, libraryID)
		if err != nil {
			return err
		}
		if err := validateManagedEntity(library, &entity); err != nil {
			return err
		}
		for _, f := range library.EntitySchema.Fields {
			if _, exists := entity.Attributes[f.Key]; exists && f.Archived {
				return apperrors.NewBadRequestError("archived fields cannot be assigned")
			}
		}
		if err := validateManagedAttributeReferences(tx, library, entity.Attributes); err != nil {
			return err
		}
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		if err := saveManagedAttributeVersion(ctx, tx, &entity, input.EffectiveAt); err != nil {
			return err
		}
		if err := saveManagedAliases(tx, &entity, input.Aliases); err != nil {
			return err
		}
		if err := queueManagedEntity(tx, &entity); err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "create_entity", entity.ID, nil, entity); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "managed_entity_created")
		}
		return nil
	})
	return &entity, managedWriteError(err)
}

func (s *EntityService) UpdateManagedEntity(ctx context.Context, id string, patch types.ManagedEntityPatch) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("entity editor required")
	}
	if patch.EffectiveAt != nil && patch.Attributes == nil {
		return nil, apperrors.NewBadRequestError("effective_at requires attributes")
	}
	var entity types.Entity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Where("id=? AND space_id=? AND merged_into_id=''", id, spaceID).First(&entity).Error; err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, entity.LibraryID)
		if err != nil {
			return err
		}
		if patch.Revision != entity.Revision {
			return apperrors.NewConflictError("entity changed; reload before saving")
		}
		if err := tx.Model(&types.EntityAlias{}).Where("entity_id=? AND space_id=?", id, spaceID).Order("alias").Pluck("alias", &entity.Aliases).Error; err != nil {
			return err
		}
		if err := hydrateManagedEntityAttributes(ctx, tx, []*types.Entity{&entity}, time.Now()); err != nil {
			return err
		}
		before := entity
		if patch.Name != nil {
			entity.CanonicalName = *patch.Name
		}
		if patch.Kind != nil {
			entity.Kind = *patch.Kind
		}
		if patch.Code != nil {
			entity.Code = *patch.Code
		}
		if patch.Description != nil {
			entity.Description = *patch.Description
		}
		if patch.Attributes != nil {
			entity.Attributes = *patch.Attributes
		}
		if err := validateManagedEntity(library, &entity); err != nil {
			return err
		}
		if err := validateManagedAttributeReferences(tx, library, entity.Attributes); err != nil {
			return err
		}
		if entity.Kind != before.Kind {
			var versions []types.EntityAttributeVersion
			if err := tx.Where("entity_id=? AND space_id=?", id, spaceID).Find(&versions).Error; err != nil {
				return err
			}
			for _, version := range versions {
				if err := library.EntitySchema.ValidateAttributes(entity.Kind, version.Attributes); err != nil {
					return apperrors.NewBadRequestError("kind change would invalidate attribute history: " + err.Error())
				}
			}
		}
		for _, f := range library.EntitySchema.Fields {
			if f.Archived && !reflect.DeepEqual(before.Attributes[f.Key], entity.Attributes[f.Key]) {
				return apperrors.NewBadRequestError("archived fields cannot be changed")
			}
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		if patch.Archived != nil && *patch.Archived && entity.ArchivedAt == nil {
			var children int64
			if err := tx.Table("relations r").Joins("JOIN library_relation_types t ON t.library_id=r.library_id AND t.name=r.relation_type AND t.hierarchical").Joins("JOIN entities e ON e.id=r.subject_id AND e.archived_at IS NULL").Where("r.object_id=? AND r.library_id=?", id, entity.LibraryID).Count(&children).Error; err != nil {
				return err
			}
			if children > 0 {
				return apperrors.NewBadRequestError("move or archive child entities before archiving their parent")
			}
			now := time.Now()
			entity.ArchivedAt = &now
		} else if patch.Archived != nil && !*patch.Archived {
			entity.ArchivedAt = nil
		}
		entity.Revision++
		if before.CanonicalName != entity.CanonicalName || before.Description != entity.Description {
			entity.EmbeddingNeedsRebuild = true
		}
		entity.UpdatedAt = time.Now()
		if err := tx.Save(&entity).Error; err != nil {
			return err
		}
		if patch.Attributes != nil {
			if err := saveManagedAttributeVersion(ctx, tx, &entity, patch.EffectiveAt); err != nil {
				return err
			}
		}
		aliases := before.Aliases
		if patch.Aliases != nil {
			aliases = *patch.Aliases
		}
		if entity.CanonicalName != before.CanonicalName {
			aliases = append(append([]string{}, aliases...), before.CanonicalName)
		}
		if err := saveManagedAliases(tx, &entity, aliases); err != nil {
			return err
		}
		var relations []types.Relation
		if err := tx.Where("space_id=? AND (subject_id=? OR object_id=?)", spaceID, id, id).Find(&relations).Error; err != nil {
			return err
		}
		if entity.ArchivedAt == nil {
			for _, r := range relations {
				peerID := r.SubjectID
				if peerID == id {
					peerID = r.ObjectID
				}
				var peer types.Entity
				if err := tx.Where("id=? AND space_id=?", peerID, spaceID).First(&peer).Error; err != nil {
					return err
				}
				if peer.ArchivedAt != nil {
					rule, _ := managedRelationType(library, r.RelationType)
					if rule.Hierarchical && r.SubjectID == id && before.ArchivedAt != nil {
						return apperrors.NewBadRequestError("restore the hierarchy parent first")
					}
					continue
				}
				if err := validateManagedRelation(tx, library, r, r.ID); err != nil {
					return err
				}
			}
		}
		if err := queueManagedEntity(tx, &entity); err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "update_entity", id, before, entity); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil && (before.CanonicalName != entity.CanonicalName || before.Description != entity.Description) {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "managed_entity_updated")
		}
		return nil
	})
	if err == nil {
		err = s.refreshArticleHeadersForEntity(ctx, spaceID, id)
	}
	return &entity, managedWriteError(err)
}

func (s *EntityService) ManagedEntityHistory(ctx context.Context, id string, page, pageSize int) (*types.OperationLogPage, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, spaceID).First(&entity).Error; err != nil {
		return nil, managedWriteError(err)
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, apperrors.NewBadRequestError("invalid history page")
	}
	query := s.db.WithContext(ctx).Model(&types.OperationLog{}).Where("space_id=?", spaceID).
		Where("object_id=? OR before_value->>'subject_id'=? OR before_value->>'object_id'=? OR after_value->>'subject_id'=? OR after_value->>'object_id'=? OR after_value->>'entity_id'=? OR before_value->>'source_id'=? OR before_value->>'target_id'=? OR after_value->>'source_id'=? OR after_value->>'target_id'=?", id, id, id, id, id, id, id, id, id, id)
	result := &types.OperationLogPage{Items: []types.OperationLog{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	err = query.Order("created_at DESC,id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&result.Items).Error
	return result, err
}

func requireExtractedEntityWriter(tx *gorm.DB, libraryID string) error {
	var library types.Library
	if err := tx.Select("entity_mode").Where("id=?", libraryID).First(&library).Error; err != nil {
		return err
	}
	if library.ManagesEntities() {
		return apperrors.NewBadRequestError("use the managed entity editor with its revision; merging managed entities is unavailable")
	}
	return nil
}
