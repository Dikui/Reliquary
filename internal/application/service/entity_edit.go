package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"sort"
	"strings"
	"time"

	werrors "reliquary/internal/errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *EntityService) MergeEntities(ctx context.Context, keepEntityID, mergeEntityID string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return werrors.NewUnauthorizedError("authenticated merge actor required")
	}
	if keepEntityID == "" || mergeEntityID == "" || keepEntityID == mergeEntityID {
		return werrors.NewBadRequestError("two distinct entity IDs are required")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var keep, merged types.Entity
		targets := []struct {
			id     string
			target *types.Entity
		}{{keepEntityID, &keep}, {mergeEntityID, &merged}}
		sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
		for _, item := range targets {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND space_id = ?", item.id, spaceID).First(item.target).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return werrors.NewNotFoundError("entity not found")
				}
				return err
			}
		}
		if merged.MergedIntoID == keep.ID {
			return nil
		}
		if keep.MergedIntoID != "" || merged.MergedIntoID != "" {
			return werrors.NewBadRequestError("entity was already merged into a different entity")
		}
		var aliases []types.EntityAlias
		if err := tx.Where("space_id = ? AND entity_id = ?", spaceID, merged.ID).Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			if err := saveEntityAlias(tx, keep, alias.Alias, "merge"); err != nil {
				return err
			}
		}
		if err := tx.Where("space_id=? AND entity_id=?",
			spaceID,
			merged.ID).Delete(&types.EntityAlias{}).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO entry_entities (space_id, entry_id, entity_id, created_at)
            SELECT space_id, entry_id, ?, created_at FROM entry_entities WHERE space_id=? AND entity_id=?
            ON CONFLICT (entry_id, entity_id) DO NOTHING`, keep.ID, spaceID, merged.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND entity_id=?", spaceID, merged.ID).Delete(&types.EntryEntity{}).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO follows (space_id, target_type, target_id, created_at)
            SELECT space_id, 'entity', ?, created_at FROM follows WHERE space_id=? AND target_type='entity' AND target_id=?
            ON CONFLICT (space_id, target_type, target_id) DO UPDATE
            SET created_at=LEAST(follows.created_at, EXCLUDED.created_at)`, keep.ID, spaceID, merged.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND target_type='entity' AND target_id=?", spaceID, merged.ID).Delete(&types.Follow{}).Error; err != nil {
			return err
		}
		if err := saveEntityAlias(tx, keep, merged.CanonicalName, "merge"); err != nil {
			return err
		}
		for _, column := range []string{"subject_entity_id", "object_entity_id"} {
			if err := tx.Model(&types.Fact{}).Where("space_id=? AND "+column+"=?", spaceID, merged.ID).
				Update(column, keep.ID).Error; err != nil {
				return err
			}
		}
		actor, _ := types.PrincipalFromContext(ctx)
		record := types.EntityMergeRecord{
			ID:            uuid.NewString(),
			SpaceID:       spaceID,
			KeepEntityID:  keep.ID,
			MergeEntityID: merged.ID,
			Actor:         actor.StorageID(),
			CreatedAt:     time.Now(),
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Entity{}).Where("space_id=? AND merged_into_id=?",
			spaceID,
			merged.ID).Update("merged_into_id",
			keep.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&merged).Update("merged_into_id", keep.ID).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "merge_entities", keep.ID,
			map[string]any{"keep_entity_id": keep.ID, "merge_entity_id": merged.ID, "merged_into_id": ""},
			map[string]any{"keep_entity_id": keep.ID, "merge_entity_id": merged.ID, "merged_into_id": keep.ID}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.refreshArticleHeadersForEntity(ctx, spaceID, keepEntityID); err != nil {
		return err
	}
	return nil
}

func (s *EntityService) refreshArticleHeadersForEntity(ctx context.Context, spaceID uint64, entityID string) error {
	if s.headerRefresher == nil || entityDatabaseInTransaction(s.db) {
		return nil
	}
	var ids []string
	if err := s.db.WithContext(ctx).Model(&types.EntryEntity{}).
		Where("space_id=? AND entity_id=?", spaceID, entityID).Pluck("entry_id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		s.headerRefresher.ScheduleRefresh(ctx, id)
	}
	return nil
}

func entityDatabaseInTransaction(db *gorm.DB) bool {
	if db == nil || db.Statement == nil {
		return false
	}
	_, ok := db.Statement.ConnPool.(gorm.TxCommitter)
	return ok
}

func saveEntityAlias(tx *gorm.DB, entity types.Entity, name, method string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	alias := types.EntityAlias{
		ID:               uuid.NewString(),
		SpaceID:          entity.SpaceID,
		EntityID:         entity.ID,
		Alias:            name,
		NormalizedAlias:  types.NormalizeEntityKey(name),
		ResolutionMethod: method,
		CreatedAt:        time.Now(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "space_id"},
			{Name: "entity_id"},
			{Name: "normalized_alias"},
		},
		DoNothing: true,
	}).
		Create(&alias).
		Error
}

func validateEntityInput(input types.EntityInput) (types.EntityInput, error) {
	input.CanonicalName = strings.TrimSpace(input.CanonicalName)
	if types.NormalizeEntityKey(input.CanonicalName) == "" || len([]rune(input.CanonicalName)) > 512 ||
		len([]rune(types.NormalizeEntityKey(input.CanonicalName))) > 512 ||
		strings.ContainsAny(input.CanonicalName, "\r\n\x00") {
		return input, werrors.NewBadRequestError("entity name must be a single line of 1 to 512 characters")
	}
	aliases := []string{}
	seen := map[string]bool{types.NormalizeEntityKey(input.CanonicalName): true}
	for _, alias := range input.Aliases {
		alias = strings.TrimSpace(alias)
		key := types.NormalizeEntityKey(alias)
		if key == "" || len([]rune(alias)) > 512 || len([]rune(key)) > 512 || strings.ContainsAny(alias, "\r\n\x00") {
			return input, werrors.NewBadRequestError("alias must be a single line of 1 to 512 characters")
		}
		if !seen[key] {
			aliases = append(aliases, alias)
			seen[key] = true
		}
	}
	input.Aliases = aliases
	return input, nil
}

// SaveEntity replaces the editable name and aliases in one transaction.
func (s *EntityService) SaveEntity(ctx context.Context, id string, input types.EntityInput) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	input, err = validateEntityInput(input)
	if err != nil {
		return nil, err
	}
	entity := types.Entity{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if id != "" {
			if err := tx.Where("space_id=? AND id=? AND merged_into_id=''",
				spaceID,
				id).First(&entity).Error; errors.Is(
				err,
				gorm.ErrRecordNotFound,
			) {
				return werrors.NewNotFoundError("entity not found")
			} else if err != nil {
				return err
			}
		} else {
			entity = types.Entity{ID: uuid.NewString(), SpaceID: spaceID, CreatedAt: time.Now()}
		}
		normalizedName := types.NormalizeEntityKey(input.CanonicalName)
		if id == "" || entity.NormalizedName != normalizedName {
			var count int64
			if err := tx.Model(&types.Entity{}).
				Where("space_id=? AND normalized_name=? AND id<>? AND merged_into_id=''",
					spaceID, normalizedName, entity.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return werrors.NewBadRequestError("entity name conflicts with another entity")
			}
		}
		entity.CanonicalName, entity.NormalizedName = input.CanonicalName, normalizedName
		entity.UpdatedAt = time.Now()
		if err := tx.Save(&entity).Error; err != nil {
			return err
		}
		var oldAliases []types.EntityAlias
		if err := tx.Where("space_id=? AND entity_id=?", spaceID, entity.ID).Find(&oldAliases).Error; err != nil {
			return err
		}
		oldKeys := map[string]bool{}
		for _, alias := range oldAliases {
			oldKeys[alias.NormalizedAlias] = true
		}
		addedAliases := []string{}
		for _, alias := range input.Aliases {
			if !oldKeys[types.NormalizeEntityKey(alias)] {
				addedAliases = append(addedAliases, alias)
			}
		}
		if err := tx.Where("space_id=? AND entity_id=?",
			spaceID,
			entity.ID).Delete(&types.EntityAlias{}).Error; err != nil {
			return err
		}
		for _, alias := range input.Aliases {
			if err := saveEntityAlias(tx, entity, alias, "manual"); err != nil {
				return err
			}
		}
		entity.Aliases = input.Aliases
		if id != "" && len(addedAliases) > 0 {
			if err := backfillFactAliases(ctx, tx, entity, addedAliases); err != nil {
				return err
			}
			return NewOperationLogService(tx).RecordTx(ctx, tx, "add_entity_alias", entity.ID,
				oldAliases, map[string]any{"aliases": entity.Aliases, "added_aliases": addedAliases})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &entity, s.refreshArticleHeadersForEntity(ctx, spaceID, entity.ID)
}

// DeleteEntity removes the entity, redirects, mentions and follows.
func (s *EntityService) DeleteEntity(ctx context.Context, id string) error {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		var entity types.Entity
		if err := tx.Where("space_id=? AND id=? AND merged_into_id=''", spaceID, id).First(&entity).Error; errors.Is(
			err,
			gorm.ErrRecordNotFound,
		) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		// Old merge URLs must not resolve to a deleted target.
		var ids []string
		if err := tx.Model(&types.Entity{}).Where("space_id=? AND (id=? OR merged_into_id=?)",
			spaceID,
			id,
			id).Pluck("id",
			&ids).Error; err != nil {
			return err
		}
		if err := tx.Where("space_id=? AND entity_id IN ?",
			spaceID,
			ids).Delete(&types.EntityAlias{}).Error; err != nil {
			return err
		}
		return tx.Where("space_id=? AND id IN ?", spaceID, ids).Delete(&types.Entity{}).Error
	})
}
