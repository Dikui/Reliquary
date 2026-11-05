package service

import (
	"context"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/application/graph"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func validateManagedRelation(tx *gorm.DB, library *types.Library, relation types.Relation, excludeID string) error {
	if relation.SubjectID == relation.ObjectID {
		return apperrors.NewBadRequestError("an entity cannot relate to itself")
	}
	rule, ok := managedRelationType(library, relation.RelationType)
	if !ok || !rule.Enabled {
		return apperrors.NewBadRequestError("relationship type is not enabled in this library")
	}
	if err := types.ValidateFieldAttributes(rule.Fields, relation.Attributes); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	if err := validateAttributeReferences(tx, library.SpaceID, library.ID, rule.Fields, relation.Attributes); err != nil {
		return err
	}
	var subject, object types.Entity
	for id, entity := range map[string]*types.Entity{relation.SubjectID: &subject, relation.ObjectID: &object} {
		if err := tx.Where("id=? AND space_id=? AND library_id=? AND archived_at IS NULL AND merged_into_id=''", id, library.SpaceID, library.ID).First(entity).Error; err != nil {
			return managedWriteError(err)
		}
	}
	if err := validateManagedEndpoints(library, rule, &subject, &object); err != nil {
		return err
	}
	if !rule.Hierarchical {
		return nil
	}
	var count int64
	if err := tx.Table("relations r").Where("r.library_id=? AND r.subject_id=? AND r.relation_type=? AND r.id<>? AND r.valid_to IS NULL", library.ID, relation.SubjectID, relation.RelationType, excludeID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return apperrors.NewBadRequestError("an entity can have only one hierarchy parent")
	}
	var cycle bool
	err := tx.Raw(`WITH RECURSIVE ancestors(id) AS (
 SELECT ?::varchar UNION
 SELECT r.object_id FROM relations r JOIN ancestors a ON r.subject_id=a.id
 WHERE r.library_id=? AND r.relation_type=? AND r.id<>? AND r.valid_to IS NULL
) SELECT EXISTS (SELECT 1 FROM ancestors WHERE id=?)`, relation.ObjectID, library.ID, relation.RelationType, excludeID, relation.SubjectID).Scan(&cycle).Error
	if err != nil {
		return err
	}
	if cycle {
		return apperrors.NewBadRequestError("hierarchy relationships cannot form a cycle")
	}
	return nil
}

func queueManagedRelation(tx *gorm.DB, relation types.Relation, deleted bool) error {
	for _, id := range []string{relation.SubjectID, relation.ObjectID} {
		if err := tx.Create(&types.GraphPendingSync{ID: uuid.NewString(), SpaceID: relation.SpaceID, EntityID: id, RelationID: relation.ID, RelationVersion: relation.Version, Deleted: deleted, CreatedAt: time.Now()}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *EntityService) SaveManagedRelation(ctx context.Context, libraryID, id string, input types.ManagedRelationInput) (*types.Relation, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, apperrors.NewUnauthorizedError("relationship editor required")
	}
	var row types.Relation
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if id != "" {
			if err := tx.Where("id=? AND space_id=?", id, spaceID).First(&row).Error; err != nil {
				return err
			}
			libraryID = row.LibraryID
			if row.ValidTo != nil || row.Version != input.Version {
				return apperrors.NewConflictError("relationship changed; reload before saving")
			}
		}
		library, err := managedLibrary(tx, spaceID, libraryID)
		if err != nil {
			return err
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		before := row
		row.LibraryID, row.SpaceID = libraryID, spaceID
		row.SubjectID, row.RelationType, row.ObjectID = input.SubjectID, input.RelationType, input.ObjectID
		if input.Attributes != nil {
			row.Attributes = input.Attributes
		}
		if row.Attributes == nil {
			row.Attributes = map[string]any{}
		}
		if rule, ok := managedRelationType(library, row.RelationType); ok {
			for _, field := range rule.Fields {
				if field.Archived && !reflect.DeepEqual(before.Attributes[field.Key], row.Attributes[field.Key]) {
					return apperrors.NewBadRequestError("archived relationship fields cannot be changed")
				}
			}
		}
		if err := validateManagedRelation(tx, library, row, id); err != nil {
			return err
		}
		row.UpdatedAt = time.Now()
		operation := "create_relation"
		if id != "" {
			operation = "update_relation"
			if err := endManagedRelation(tx, before, row.UpdatedAt); err != nil {
				return err
			}
		}
		row.ID, row.CreatedAt, row.ValidFrom, row.Version = uuid.NewString(), row.UpdatedAt, row.UpdatedAt, 1
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if id != "" {
			if err := copyManagedRelationSources(tx, before.ID, row.ID); err != nil {
				return err
			}
		}
		if err := queueManagedRelation(tx, row, false); err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, operation, row.ID, before, row)
	})
	return &row, managedWriteError(err)
}

func (s *EntityService) DeleteManagedRelation(ctx context.Context, id string, version int64) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return apperrors.NewUnauthorizedError("relationship editor required")
	}
	return managedWriteError(s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var row types.Relation
		if err := tx.Where("space_id=? AND id=?", spaceID, id).First(&row).Error; err != nil {
			return err
		}
		if _, err := managedLibrary(tx, spaceID, row.LibraryID); err != nil {
			return err
		}
		if row.ValidTo != nil || row.Version != version {
			return apperrors.NewConflictError("relationship changed; reload before deleting")
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		before := row
		if err := endManagedRelation(tx, row, time.Now()); err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "delete_relation", id, before, nil)
	}))
}

// Ending a row preserves the structure and evidence visible before this change.
func endManagedRelation(tx *gorm.DB, row types.Relation, at time.Time) error {
	row.ValidTo, row.UpdatedAt = &at, at
	row.Version++
	if err := tx.Model(&row).Select("valid_to", "updated_at", "version").Updates(&row).Error; err != nil {
		return err
	}
	return queueManagedRelation(tx, row, true)
}

func copyManagedRelationSources(tx *gorm.DB, sourceID, targetID string) error {
	var sources []types.RelationSource
	if err := tx.Where("relation_id=?", sourceID).Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		source.RelationID = targetID
		if err := tx.Create(&source).Error; err != nil {
			return err
		}
	}
	return nil
}
