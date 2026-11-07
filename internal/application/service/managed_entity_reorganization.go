package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/application/graph"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func reorganizationTime(at *time.Time) error {
	if at != nil && (at.IsZero() || at.After(time.Now())) {
		return apperrors.NewBadRequestError("reorganization effective_at must not be in the future")
	}
	return nil
}

func loadReorganizationEntity(tx *gorm.DB, spaceID uint64, id string, revision int64) (*types.Entity, error) {
	var entity types.Entity
	if err := tx.Where("space_id=? AND id=? AND merged_into_id='' AND archived_at IS NULL", spaceID, id).First(&entity).Error; err != nil {
		return nil, managedWriteError(err)
	}
	if entity.Revision != revision {
		return nil, apperrors.NewConflictError("entity changed; reload before reorganizing")
	}
	return &entity, nil
}

// End moved relationships and create their successors so past structures remain queryable.
func moveManagedRelations(tx *gorm.DB, library *types.Library, source, target string, ids []string, all, copyRelations bool) error {
	var rows []types.Relation
	query := tx.Where("space_id=? AND valid_to IS NULL AND (subject_id=? OR object_id=?)", library.SpaceID, source, source)
	if !all {
		if len(ids) == 0 {
			return nil
		}
		query = query.Where("id IN ?", ids)
	}
	if err := query.Find(&rows).Error; err != nil {
		return err
	}
	if !all && len(rows) != len(ids) {
		return apperrors.NewBadRequestError("relation_ids must name distinct source relationships")
	}
	for i := range rows {
		row := &rows[i]
		before := *row
		if row.SubjectID == source {
			row.SubjectID = target
		}
		if row.ObjectID == source {
			row.ObjectID = target
		}
		if row.SubjectID == row.ObjectID {
			return apperrors.NewConflictError("reorganization would create a self relationship")
		}
		var count int64
		if err := tx.Model(&types.Relation{}).Where("space_id=? AND library_id=? AND subject_id=? AND relation_type=? AND object_id=? AND id<>? AND valid_to IS NULL", library.SpaceID, library.ID, row.SubjectID, row.RelationType, row.ObjectID, row.ID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewConflictError("reorganization would merge distinct relationship histories")
		}
		row.UpdatedAt = time.Now()
		if !copyRelations {
			if err := endManagedRelation(tx, before, row.UpdatedAt); err != nil {
				return err
			}
		}
		row.ID, row.Version, row.CreatedAt, row.ValidFrom = uuid.NewString(), 1, row.UpdatedAt, row.UpdatedAt
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if err := copyManagedRelationSources(tx, before.ID, row.ID); err != nil {
			return err
		}
		if err := queueManagedRelation(tx, *row, false); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := validateManagedRelation(tx, library, row, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// The caller holds the identity and graph locks; deletion shares the merge's
// transaction so a later conflict also restores evidence and pending deltas.
func dropManagedMergeRelations(ctx context.Context, tx *gorm.DB, library *types.Library, source string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []types.Relation
	if err := tx.Where("space_id=? AND library_id=? AND (subject_id=? OR object_id=?) AND id IN ? AND valid_to IS NULL", library.SpaceID, library.ID, source, source, ids).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(ids) {
		return apperrors.NewBadRequestError("drop_relation_ids must name distinct relationships of the source entity")
	}
	for _, row := range rows {
		before := row
		if err := endManagedRelation(tx, row, time.Now()); err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "delete_relation", row.ID, before, nil); err != nil {
			return err
		}
	}
	return nil
}

func mergeManagedEntryLinks(tx *gorm.DB, spaceID uint64, source, target, preference string) error {
	var links []types.EntryEntity
	if err := tx.Where("space_id=? AND entity_id=?", spaceID, source).Find(&links).Error; err != nil {
		return err
	}
	for _, link := range links {
		var existing types.EntryEntity
		err := tx.Where("space_id=? AND entity_id=? AND entry_id=?", spaceID, target, link.EntryID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entity_id=? AND entry_id=?", spaceID, source, link.EntryID).Update("entity_id", target).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if existing.Excluded != link.Excluded {
			return apperrors.NewConflictError("document associations disagree; resolve them before merging")
		}
		if existing.RelationType != link.RelationType {
			switch preference {
			case "source":
				existing.RelationType = link.RelationType
			case "target":
			default:
				return apperrors.NewConflictError("document relationship types disagree; choose source or target preference")
			}
		}
		if existing.BodyHash != link.BodyHash && len(existing.Evidence) > 0 && len(link.Evidence) > 0 {
			return apperrors.NewConflictError("document evidence comes from different body versions")
		}
		if len(existing.Evidence) == 0 && len(link.Evidence) > 0 {
			existing.BodyHash = link.BodyHash
		}
		seen := map[string]bool{}
		for _, evidence := range existing.Evidence {
			data, _ := json.Marshal(evidence)
			seen[string(data)] = true
		}
		for _, evidence := range link.Evidence {
			data, _ := json.Marshal(evidence)
			if !seen[string(data)] {
				existing.Evidence = append(existing.Evidence, evidence)
				seen[string(data)] = true
			}
		}
		if link.Origin == "manual" {
			existing.Origin = "manual"
		}
		if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entity_id=? AND entry_id=?", spaceID, target, link.EntryID).Select("origin", "evidence", "body_hash", "relation_type").Updates(&existing).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND entity_id=? AND entry_id=?", spaceID, source, link.EntryID).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityService) MergeManagedEntities(ctx context.Context, id string, input types.ManagedEntityMergeInput) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("entity editor required")
	}
	if id == input.SourceID || input.SourceID == "" || input.Attributes == nil {
		return nil, apperrors.NewBadRequestError("distinct source_id and explicit resulting attributes are required")
	}
	if input.DocumentRelationPreference != "" && input.DocumentRelationPreference != "source" && input.DocumentRelationPreference != "target" {
		return nil, apperrors.NewBadRequestError("document_relation_preference must be source or target")
	}
	if err := reorganizationTime(input.EffectiveAt); err != nil {
		return nil, err
	}
	var target *types.Entity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var err error
		target, err = loadReorganizationEntity(tx, spaceID, id, input.Revision)
		if err != nil {
			return err
		}
		source, err := loadReorganizationEntity(tx, spaceID, input.SourceID, input.SourceRevision)
		if err != nil {
			return err
		}
		if target.LibraryID != source.LibraryID {
			return apperrors.NewBadRequestError("merged entities must belong to the same library")
		}
		library, err := managedLibrary(tx, spaceID, target.LibraryID)
		if err != nil {
			return err
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		if err := dropManagedMergeRelations(ctx, tx, library, source.ID, input.DropRelationIDs); err != nil {
			return err
		}
		if err := hydrateManagedEntityAttributes(ctx, tx, []*types.Entity{target, source}, time.Now()); err != nil {
			return err
		}
		beforeTarget, beforeSource := *target, *source
		matching := make(map[string]bool, len(source.AutoMatch)+len(target.AutoMatch))
		for spelling, enabled := range source.AutoMatch {
			matching[normalizeEntityName(spelling)] = enabled
		}
		for spelling, enabled := range target.AutoMatch {
			matching[normalizeEntityName(spelling)] = enabled
		}
		target.AutoMatch = matching
		target.Attributes = input.Attributes
		if err := validateManagedEntity(library, target); err != nil {
			return err
		}
		for _, field := range library.EntitySchema.Fields {
			if field.Archived && !reflect.DeepEqual(beforeTarget.Attributes[field.Key], target.Attributes[field.Key]) {
				return apperrors.NewBadRequestError("archived fields cannot be changed")
			}
		}
		now := time.Now()
		source.MergedIntoID, source.ArchivedAt, source.UpdatedAt = id, &now, now
		source.Revision++
		if err := tx.Save(source).Error; err != nil {
			return err
		}
		if err := validateManagedAttributeReferences(tx, library, target.Attributes); err != nil {
			return err
		}
		target.Revision++
		target.UpdatedAt = now
		target.EmbeddingNeedsRebuild = true
		if err := tx.Save(target).Error; err != nil {
			return err
		}
		if err := saveManagedAttributeVersion(ctx, tx, target, input.EffectiveAt); err != nil {
			return err
		}
		if err := mergeManagedEntryLinks(tx, spaceID, source.ID, target.ID, input.DocumentRelationPreference); err != nil {
			return err
		}
		if err := moveManagedRelations(tx, library, source.ID, target.ID, nil, true, false); err != nil {
			return err
		}
		var aliases []string
		if err := tx.Model(&types.EntityAlias{}).Where("space_id=? AND entity_id IN ?", spaceID, []string{source.ID, target.ID}).Pluck("alias", &aliases).Error; err != nil {
			return err
		}
		aliases = append(aliases, source.CanonicalName)
		if source.Code != "" {
			aliases = append(aliases, source.Code)
		}
		if err := tx.Where("space_id=? AND entity_id=?", spaceID, source.ID).Delete(&types.EntityAlias{}).Error; err != nil {
			return err
		}
		if err := saveManagedAliases(tx, library, target, aliases, input.NameConfirmations); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO follows (space_id,target_type,target_id,created_at) SELECT space_id,'entity',?,created_at FROM follows WHERE space_id=? AND target_type='entity' AND target_id=? ON CONFLICT (space_id,target_type,target_id) DO UPDATE SET created_at=LEAST(follows.created_at,EXCLUDED.created_at)`, target.ID, spaceID, source.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND target_type='entity' AND target_id=?", spaceID, source.ID).Delete(&types.Follow{}).Error; err != nil {
			return err
		}
		record := types.EntityReorganization{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: library.ID, Kind: "merge", SourceID: source.ID, TargetID: target.ID, CreatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "merge_managed_entities", target.ID, map[string]any{"source_id": source.ID, "target_id": target.ID, "source": beforeSource, "target": beforeTarget}, record); err != nil {
			return err
		}
		if err := queueManagedEntity(tx, source); err != nil {
			return err
		}
		if err := queueManagedEntity(tx, target); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "managed_entity_merged")
		}
		return nil
	})
	if err == nil {
		err = s.refreshArticleHeadersForEntity(ctx, spaceID, id)
	}
	return target, managedWriteError(err)
}

func (s *EntityService) SplitManagedEntity(ctx context.Context, id string, input types.ManagedEntitySplitInput) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("entity editor required")
	}
	if input.NewEntity.Attributes == nil {
		return nil, apperrors.NewBadRequestError("new_entity requires explicit attributes")
	}
	if input.NewEntity.EffectiveAt != nil {
		return nil, apperrors.NewBadRequestError("use the split effective_at for the resulting entity")
	}
	if err := reorganizationTime(input.EffectiveAt); err != nil {
		return nil, err
	}
	var target *types.Entity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		source, err := loadReorganizationEntity(tx, spaceID, id, input.Revision)
		if err != nil {
			return err
		}
		library, err := managedLibrary(tx, spaceID, source.LibraryID)
		if err != nil {
			return err
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		now := time.Now()
		in := input.NewEntity
		target = &types.Entity{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: library.ID, EntityType: "product", Kind: in.Kind, Code: in.Code, CanonicalName: in.Name, Description: in.Description, Attributes: in.Attributes, AutoMatch: in.AutoMatch, Revision: 1, EmbeddingNeedsRebuild: true, CreatedAt: now, UpdatedAt: now}
		if err := validateManagedEntity(library, target); err != nil {
			return err
		}
		if err := validateManagedAttributeReferences(tx, library, target.Attributes); err != nil {
			return err
		}
		for _, field := range library.EntitySchema.Fields {
			if _, exists := target.Attributes[field.Key]; exists && field.Archived {
				return apperrors.NewBadRequestError("archived fields cannot be assigned")
			}
		}
		if err := tx.Create(target).Error; err != nil {
			return err
		}
		if err := saveManagedAliases(tx, library, target, in.Aliases, in.NameConfirmations); err != nil {
			return err
		}
		if err := saveManagedAttributeVersion(ctx, tx, target, input.EffectiveAt); err != nil {
			return err
		}
		if len(input.EntryIDs) > 0 {
			var count int64
			if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entity_id=? AND entry_id IN ?", spaceID, id, input.EntryIDs).Count(&count).Error; err != nil {
				return err
			}
			if int(count) != len(input.EntryIDs) {
				return apperrors.NewBadRequestError("entry_ids must name distinct source associations")
			}
			if err := tx.Model(&types.EntryEntity{}).Where("space_id=? AND entity_id=? AND entry_id IN ?", spaceID, id, input.EntryIDs).Update("entity_id", target.ID).Error; err != nil {
				return err
			}
		}
		if err := moveManagedRelations(tx, library, id, target.ID, input.RelationIDs, false, false); err != nil {
			return err
		}
		if err := moveManagedRelations(tx, library, id, target.ID, input.CopyRelationIDs, false, true); err != nil {
			return err
		}
		source.Revision++
		source.UpdatedAt = now
		if err := tx.Model(source).Select("revision", "updated_at").Updates(source).Error; err != nil {
			return err
		}
		record := types.EntityReorganization{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: library.ID, Kind: "split", SourceID: id, TargetID: target.ID, CreatedAt: now}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "split_managed_entity", id, nil, record); err != nil {
			return err
		}
		if err := queueManagedEntity(tx, source); err != nil {
			return err
		}
		if err := queueManagedEntity(tx, target); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "managed_entity_split")
		}
		return nil
	})
	if err == nil && target != nil {
		err = s.refreshArticleHeadersForEntity(ctx, spaceID, target.ID)
	}
	return target, managedWriteError(err)
}
