package service

import (
	"context"
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
	if err := tx.Table("relations r").Joins("JOIN library_relation_types t ON t.library_id=r.library_id AND t.name=r.relation_type AND t.hierarchical").Where("r.library_id=? AND r.subject_id=? AND r.id<>?", library.ID, relation.SubjectID, excludeID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return apperrors.NewBadRequestError("an entity can have only one hierarchy parent")
	}
	var cycle bool
	err := tx.Raw(`WITH RECURSIVE ancestors(id) AS (
 SELECT ?::varchar UNION
 SELECT r.object_id FROM relations r JOIN ancestors a ON r.subject_id=a.id
 JOIN library_relation_types t ON t.library_id=r.library_id AND t.name=r.relation_type AND t.hierarchical
 WHERE r.library_id=? AND r.id<>?
) SELECT EXISTS (SELECT 1 FROM ancestors WHERE id=?)`, relation.ObjectID, library.ID, excludeID, relation.SubjectID).Scan(&cycle).Error
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
			if row.Version != input.Version {
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
		if err := validateManagedRelation(tx, library, row, id); err != nil {
			return err
		}
		row.UpdatedAt = time.Now()
		operation := "update_relation"
		if id == "" {
			row.ID, row.CreatedAt, row.Version = uuid.NewString(), row.UpdatedAt, 1
			operation = "create_relation"
		} else {
			row.Version++
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if id != "" {
			if err := queueManagedRelation(tx, before, false); err != nil {
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
		if row.Version != version {
			return apperrors.NewConflictError("relationship changed; reload before deleting")
		}
		if err := graph.Lock(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		before := row
		row.Version++
		if err := queueManagedRelation(tx, row, true); err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "delete_relation", id, before, nil)
	}))
}
