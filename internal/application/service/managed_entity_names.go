package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func hydrateManagedKindLabels(ctx context.Context, db *gorm.DB, entities []*types.Entity) error {
	ids := []string{}
	for _, entity := range entities {
		if !slices.Contains(ids, entity.LibraryID) {
			ids = append(ids, entity.LibraryID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var libraries []types.Library
	if err := db.WithContext(ctx).Select("id,entity_schema").Where("id IN ? AND entity_mode=?", ids, types.EntityModeManaged).Find(&libraries).Error; err != nil {
		return err
	}
	for _, entity := range entities {
		for _, library := range libraries {
			if entity.LibraryID == library.ID {
				entity.KindLabel = managedKindLabel(&library, entity.Kind)
				break
			}
		}
	}
	return nil
}

func managedKindLabel(library *types.Library, kind string) string {
	for _, configured := range library.EntitySchema.Kinds {
		if configured.Key == kind {
			return configured.Name
		}
	}
	return kind
}

// The caller holds the existing entity identity barrier and library lock. All
// spellings share one namespace within a kind, regardless of their field.
func validateManagedNames(tx *gorm.DB, library *types.Library, entity *types.Entity, aliases []string, confirmations []types.ManagedNameConfirmation) error {
	names := map[string]string{}
	if entity.ArchivedAt == nil {
		for _, name := range append(append([]string{}, aliases...), entity.CanonicalName) {
			names[normalizeEntityName(name)] = name
		}
	}
	if entity.Code != "" {
		names[normalizeEntityName(entity.Code)] = entity.Code
	}
	if len(names) == 0 {
		return nil
	}
	keys := make([]string, 0, len(names))
	for key := range names {
		keys = append(keys, key)
	}
	type owner struct {
		ID, Kind, CanonicalName, Spelling string
	}
	var owners []owner
	if err := tx.Raw(`SELECT DISTINCT e.id,e.kind,e.canonical_name,n.spelling FROM entities e
	 JOIN LATERAL (
	  SELECT e.normalized_name AS spelling,false AS is_code WHERE e.archived_at IS NULL
	  UNION SELECT a.normalized_alias,false FROM entity_aliases a WHERE a.entity_id=e.id AND e.archived_at IS NULL
	  UNION SELECT e.code,true WHERE e.code<>''
	 ) n ON true
	 WHERE e.space_id=? AND e.library_id=? AND e.id<>? AND e.merged_into_id='' AND (n.spelling IN ? OR n.is_code)
	 ORDER BY e.id,n.spelling`, entity.SpaceID, entity.LibraryID, entity.ID, keys).Scan(&owners).Error; err != nil {
		return err
	}
	// Code retains its original spelling in storage; normalize it with the same
	// Unicode and whitespace rules as names before comparing ownership.
	matched := []owner{}
	seen := map[[2]string]bool{}
	for _, other := range owners {
		other.Spelling = normalizeEntityName(other.Spelling)
		key := [2]string{other.ID, other.Spelling}
		if _, exists := names[other.Spelling]; exists && !seen[key] {
			matched = append(matched, other)
			seen[key] = true
		}
	}
	owners = matched
	// Hard conflicts take precedence; confirmation cannot override uniqueness.
	for _, other := range owners {
		if other.Kind == entity.Kind {
			return apperrors.NewConflictError("name, alias or code already exists in this kind")
		}
	}
	var pending []types.ManagedNameConflict
	for _, other := range owners {
		record := types.EntityNameConfirmation{SpaceID: entity.SpaceID, LibraryID: entity.LibraryID,
			EntityAID: entity.ID, KindA: entity.Kind, EntityBID: other.ID, KindB: other.Kind, NormalizedName: other.Spelling}
		if record.EntityAID > record.EntityBID {
			record.EntityAID, record.EntityBID = record.EntityBID, record.EntityAID
			record.KindA, record.KindB = record.KindB, record.KindA
		}
		var existing types.EntityNameConfirmation
		if err := tx.Where(&record).First(&existing).Error; err == nil {
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		confirmed := slices.ContainsFunc(confirmations, func(confirmation types.ManagedNameConfirmation) bool {
			return normalizeEntityName(confirmation.Name) == other.Spelling && confirmation.OtherEntityID == other.ID && confirmation.OtherKind == other.Kind
		})
		if !confirmed {
			pending = append(pending, types.ManagedNameConflict{
				ManagedNameConfirmation: types.ManagedNameConfirmation{Name: names[other.Spelling], OtherEntityID: other.ID, OtherKind: other.Kind},
				Kind:                    entity.Kind, KindLabel: managedKindLabel(library, entity.Kind), OtherName: other.CanonicalName, OtherKindLabel: managedKindLabel(library, other.Kind),
			})
			continue
		}
		record.ConfirmedAt = time.Now()
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record).Error; err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		return apperrors.NewConflictError("confirm shared names in other kinds before saving").WithDetails(&types.ManagedNameConflictDetails{
			Type: "cross_kind_name_confirmation_required", Conflicts: pending,
		})
	}
	return nil
}
