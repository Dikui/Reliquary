package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
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
	entity.Description = strings.TrimSpace(entity.Description)
	if strings.TrimSpace(entity.CanonicalName) == "" || (entity.Code != "" && strings.TrimSpace(entity.Code) == "") || len([]rune(entity.CanonicalName)) > 512 || len([]rune(entity.Code)) > 128 || len([]rune(entity.Description)) > 20000 {
		return apperrors.NewBadRequestError("name is required; name, code or description is too long")
	}
	entity.NormalizedName = normalizeEntityName(entity.CanonicalName)
	if entity.Attributes == nil {
		entity.Attributes = map[string]any{}
	}
	if err := library.EntitySchema.ValidateAttributes(entity.Kind, entity.Attributes); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	entity.KindLabel = managedKindLabel(library, entity.Kind)
	return nil
}

func saveManagedAliases(tx *gorm.DB, library *types.Library, entity *types.Entity, aliases []string, confirmations []types.ManagedNameConfirmation) error {
	if len(aliases) > 100 {
		return apperrors.NewBadRequestError("at most 100 aliases are allowed")
	}
	if err := tx.Where("space_id=? AND entity_id=?", entity.SpaceID, entity.ID).Delete(&types.EntityAlias{}).Error; err != nil {
		return err
	}
	entity.Aliases = []string{}
	seen := map[string]bool{}
	for _, alias := range append(aliases, entity.CanonicalName) {
		if strings.TrimSpace(alias) == "" || len([]rune(alias)) > 512 {
			return apperrors.NewBadRequestError("aliases must contain 1-512 characters")
		}
		key := normalizeEntityName(alias)
		if seen[key] {
			continue
		}
		seen[key] = true
		row := types.EntityAlias{ID: uuid.NewString(), SpaceID: entity.SpaceID, LibraryID: entity.LibraryID, EntityType: entity.EntityType, EntityID: entity.ID, Alias: alias, NormalizedAlias: key, ResolutionMethod: "manual", CreatedAt: time.Now()}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		entity.Aliases = append(entity.Aliases, alias)
	}
	if err := validateManagedNames(tx, library, entity, entity.Aliases, confirmations); err != nil {
		return err
	}
	// Removed spellings lose their overrides; unrelated edits retain them.
	seen[normalizeEntityName(entity.Code)] = true
	settings := make(map[string]bool, len(entity.AutoMatch))
	for name, enabled := range entity.AutoMatch {
		key := normalizeEntityName(name)
		if key == "" || len([]rune(name)) > 512 {
			return apperrors.NewBadRequestError("automatic matching settings require valid names")
		}
		if previous, exists := settings[key]; exists && previous != enabled {
			return apperrors.NewBadRequestError("conflicting automatic matching settings for the same name")
		}
		if seen[key] {
			settings[key] = enabled
		}
	}
	if len(entity.AutoMatch) > 102 {
		return apperrors.NewBadRequestError("too many automatic matching settings")
	}
	entity.AutoMatch = settings
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return tx.Model(&types.Entity{}).Where("id=? AND space_id=?", entity.ID, entity.SpaceID).UpdateColumn("auto_match", types.JSON(encoded)).Error
}

func queueManagedEntity(tx *gorm.DB, entity *types.Entity) error {
	// All catalog writers use this path. A persisted stamp also invalidates
	// matcher caches in other processes, including rapid edits in one transaction.
	if err := tx.Model(&types.Library{}).Where("id=? AND space_id=?", entity.LibraryID, entity.SpaceID).
		UpdateColumn("updated_at", gorm.Expr("GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')")).Error; err != nil {
		return err
	}
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
	if data, handled, err := s.shelfSingleEdit(ctx, libraryID, types.ShelfOperation{Type: "create_entity", Entity: &input}); handled {
		if err != nil {
			return nil, err
		}
		var entity types.Entity
		err = json.Unmarshal(data, &entity)
		return &entity, err
	}
	return s.createManagedEntity(ctx, libraryID, input, uuid.NewString())
}

func (s *EntityService) createManagedEntity(ctx context.Context, libraryID string, input types.ManagedEntityInput, id string) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("entity editor required")
	}
	entity := types.Entity{ID: id, SpaceID: spaceID, LibraryID: libraryID, EntityType: "product", Kind: input.Kind, Code: input.Code, CanonicalName: input.Name, Description: input.Description, Attributes: input.Attributes, AutoMatch: input.AutoMatch, Revision: 1, EmbeddingNeedsRebuild: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, libraryID)
		if err != nil {
			return err
		}
		if err := requireShelfChangeset(ctx, library); err != nil {
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
		at := time.Now()
		if input.EffectiveAt != nil {
			at = *input.EffectiveAt
		}
		if err := validateShelfEntityValues(tx, library, &entity, at); err != nil {
			return err
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
		if err := saveManagedAliases(tx, library, &entity, input.Aliases, input.NameConfirmations); err != nil {
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
	if data, handled, err := s.shelfSingleEdit(ctx, "", types.ShelfOperation{Type: "update_entity", EntityID: id, Patch: &patch}); handled {
		if err != nil {
			return nil, err
		}
		var entity types.Entity
		err = json.Unmarshal(data, &entity)
		return &entity, err
	}
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
		if patch.AutoMatch != nil {
			entity.AutoMatch = *patch.AutoMatch
		}
		if err := requireShelfChangeset(ctx, library); err != nil {
			return err
		}
		if err := validateManagedEntity(library, &entity); err != nil {
			return err
		}
		at := time.Now()
		if patch.EffectiveAt != nil {
			at = *patch.EffectiveAt
		}
		if err := validateShelfEntityValues(tx, library, &entity, at); err != nil {
			return err
		}
		if err := validateManagedAttributeReferences(tx, library, entity.Attributes); err != nil {
			return err
		}
		if entity.Kind != before.Kind {
			var pending []types.EntityAttributeVersion
			if err := tx.Where("entity_id=? AND effective_at>?", id, time.Now()).Find(&pending).Error; err != nil {
				return err
			}
			allowed := map[string]bool{}
			for _, field := range library.EntitySchema.Fields {
				allowed[field.Key] = len(field.Kinds) == 0 || slices.Contains(field.Kinds, entity.Kind)
			}
			for _, version := range pending {
				attributes := map[string]any{}
				for key, value := range version.Attributes {
					if allowed[key] {
						attributes[key] = value
					}
				}
				if !equalManagedAttributes(version.Attributes, attributes) {
					beforeVersion := version
					version.Attributes = attributes
					if err := tx.Model(&version).Select("attributes").Updates(&version).Error; err != nil {
						return err
					}
					if err := NewOperationLogService(tx).RecordTx(ctx, tx, "update_pending_entity_attributes", id, beforeVersion, version); err != nil {
						return err
					}
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
			if err := tx.Table("relations r").Joins("JOIN library_relation_types t ON t.library_id=r.library_id AND t.name=r.relation_type AND t.hierarchical").Joins("JOIN entities e ON e.id=r.subject_id AND e.archived_at IS NULL").Where("r.object_id=? AND r.library_id=? AND r.valid_to IS NULL", id, entity.LibraryID).Count(&children).Error; err != nil {
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
		if err := saveManagedAliases(tx, library, &entity, aliases, patch.NameConfirmations); err != nil {
			return err
		}
		var relations []types.Relation
		if err := tx.Where("space_id=? AND (subject_id=? OR object_id=?) AND valid_to IS NULL", spaceID, id, id).Find(&relations).Error; err != nil {
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

func (s *EntityService) DeleteManagedEntity(ctx context.Context, id string, revision int64) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return apperrors.NewUnauthorizedError("entity editor required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var entity types.Entity
		if err := tx.Where("id=? AND space_id=?", id, spaceID).First(&entity).Error; err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, entity.LibraryID)
		if err != nil {
			return err
		}
		if library.ShelfEnabled {
			return apperrors.NewBadRequestError("shelf entities must be archived through a changeset")
		}
		if revision != entity.Revision {
			return apperrors.NewConflictError("entity changed; reload before deleting")
		}
		var referenced bool
		if err := tx.Raw(`SELECT
			EXISTS (SELECT 1 FROM entry_entities WHERE entity_id=? AND excluded=false) OR
			EXISTS (SELECT 1 FROM relations WHERE subject_id=? OR object_id=?) OR
			EXISTS (SELECT 1 FROM relation_suppressions WHERE subject_id=? OR object_id=?) OR
			EXISTS (SELECT 1 FROM entity_reorganizations WHERE source_id=? OR target_id=?) OR
			EXISTS (SELECT 1 FROM entity_merge_records WHERE keep_entity_id=? OR merge_entity_id=?) OR
			EXISTS (SELECT 1 FROM entities WHERE merged_into_id=?)`, id, id, id, id, id, id, id, id, id, id).Scan(&referenced).Error; err != nil {
			return err
		}
		if referenced {
			return apperrors.NewConflictError("only products without documents, relationships or reorganization history can be deleted; exclude document links or archive the product first")
		}
		referenced, err = managedValueReferenced(tx, spaceID, id, id)
		if err != nil {
			return err
		}
		if referenced {
			return apperrors.NewConflictError("another product or relationship still references this product")
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		var excludedLinks []types.EntryEntity
		if err := tx.Where("entity_id=? AND space_id=? AND excluded=true", id, spaceID).Order("entry_id").Find(&excludedLinks).Error; err != nil {
			return err
		}
		if len(excludedLinks) > 0 {
			if err := tx.Where("entity_id=? AND space_id=? AND excluded=true", id, spaceID).Delete(&types.EntryEntity{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("entity_id=? AND space_id=?", id, spaceID).Delete(&types.EntityAttributeVersion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND (new_entity_id=? OR candidate_entity_id=?)", spaceID, id, id).Delete(&types.EntityMergeSuggestion{}).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND target_type='entity' AND target_id=?", spaceID, id).Delete(&types.Follow{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&entity).Error; err != nil {
			return err
		}
		// Avoid promoting Entity.MarshalJSON, which would omit the excluded links.
		type entityValue types.Entity
		before := struct {
			entityValue
			ExcludedLinks []types.EntryEntity `json:"excluded_links,omitempty"`
		}{entityValue: entityValue(entity), ExcludedLinks: excludedLinks}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "delete_entity", id, before, nil); err != nil {
			return err
		}
		now := time.Now()
		entity.ArchivedAt = &now
		return queueManagedEntity(tx, &entity)
	})
	return managedWriteError(err)
}

func (s *EntityService) ManagedEntityHistory(ctx context.Context, id string, page, pageSize int, sources ...types.OperationSource) (*types.OperationLogPage, error) {
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
	references := `object_id=@id OR before_value->>'subject_id'=@id OR before_value->>'object_id'=@id
 OR after_value->>'subject_id'=@id OR after_value->>'object_id'=@id OR after_value->>'entity_id'=@id
 OR before_value->>'source_id'=@id OR before_value->>'target_id'=@id
 OR after_value->>'source_id'=@id OR after_value->>'target_id'=@id`
	if entity.Kind == "model" {
		var library types.Library
		if err := s.db.WithContext(ctx).Select("shelf_enabled").Where("id=? AND space_id=?", entity.LibraryID, spaceID).First(&library).Error; err != nil {
			return nil, err
		}
		if library.ShelfEnabled {
			// Use saved ownership so earlier writes remain visible after records move.
			references += ` OR before_value#>>'{attributes,certification,owner_id}'=@id
 OR after_value#>>'{attributes,certification,owner_id}'=@id
 OR before_value#>>'{document_version,owner_id}'=@id
 OR after_value#>>'{document_version,owner_id}'=@id`
		}
	}
	query := s.db.WithContext(ctx).Model(&types.OperationLog{}).Where("space_id=?", spaceID).
		Where(references, map[string]any{"id": id})
	query, err = filterOperationSource(query, sources)
	if err != nil {
		return nil, err
	}
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
