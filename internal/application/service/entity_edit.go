package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
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
		if keep.LibraryID != merged.LibraryID {
			return werrors.NewBadRequestError("entities must belong to the same library")
		}
		if err := requireExtractedEntityWriter(tx, keep.LibraryID); err != nil {
			return err
		}
		if merged.MergedIntoID == keep.ID {
			return closeMergedEntitySuggestions(tx, spaceID, keep.ID, merged.ID)
		}
		if keep.MergedIntoID != "" || merged.MergedIntoID != "" {
			return werrors.NewBadRequestError("entity was already merged into a different entity")
		}
		var aliases []types.EntityAlias
		if err := tx.Where("space_id = ? AND entity_id = ?", spaceID, merged.ID).Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			var collision types.EntityAlias
			err := tx.Where("space_id = ? AND library_id = ? AND entity_type = ? AND normalized_alias = ? AND entity_id <> ?", spaceID, keep.LibraryID, keep.EntityType, alias.NormalizedAlias, merged.ID).First(&collision).Error
			if err == nil {
				if collision.EntityID != keep.ID {
					return werrors.NewBadRequestError("merge alias conflicts with another library entity")
				}
				if err := tx.Delete(&alias).Error; err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&alias).Updates(map[string]any{"entity_id": keep.ID, "entity_type": keep.EntityType}).Error; err != nil {
				return err
			}
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
		actor, _ := types.PrincipalFromContext(ctx)
		record := types.EntityMergeRecord{ID: uuid.NewString(), SpaceID: spaceID, KeepEntityID: keep.ID, MergeEntityID: merged.ID, Actor: actor.StorageID(), CreatedAt: time.Now()}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := graph.MergeEntityTx(tx, spaceID, merged.ID, keep.ID); err != nil {
			return err
		}
		if err := tx.Model(&merged).Update("merged_into_id", keep.ID).Error; err != nil {
			return err
		}
		if err := closeMergedEntitySuggestions(tx, spaceID, keep.ID, merged.ID); err != nil {
			return err
		}
		for _, id := range []string{merged.ID, keep.ID} {
			if err := notifyEntityProjection(tx, spaceID, id); err != nil {
				return err
			}
		}
		if err := tx.Model(&keep).Updates(map[string]any{"embedding_needs_rebuild": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "merge_entities", keep.ID,
			map[string]any{"keep_entity_id": keep.ID, "merge_entity_id": merged.ID, "merged_into_id": ""},
			map[string]any{"keep_entity_id": keep.ID, "merge_entity_id": merged.ID, "merged_into_id": keep.ID}); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "entity_merge")
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

// RenameEntity preserves the old canonical spelling as an alias and schedules
// a header refresh for every article linked to this identity.
func (s *EntityService) RenameEntity(ctx context.Context, entityID, name string) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 512 {
		return nil, werrors.NewBadRequestError("entity name must contain 1 to 512 characters")
	}
	var entity types.Entity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=? AND merged_into_id=''", entityID, spaceID).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		if err := requireExtractedEntityWriter(tx, entity.LibraryID); err != nil {
			return err
		}
		var collisions int64
		// Extracted names remain reserved by merged identities, including when
		// their aliases moved to a target of another type.
		if err := tx.Model(&types.Entity{}).Where("space_id=? AND library_id=? AND entity_type=? AND normalized_name=? AND id<>?", spaceID, entity.LibraryID, entity.EntityType, normalizeEntityName(name), entity.ID).Count(&collisions).Error; err != nil {
			return err
		}
		if collisions > 0 {
			return werrors.NewBadRequestError("entity name conflicts with another entity")
		}
		if err := saveEntityAlias(tx, entity, name, "rename"); err != nil {
			return err
		}
		if err := saveEntityAlias(tx, entity, entity.CanonicalName, "rename"); err != nil {
			return err
		}
		if err := tx.Model(&entity).Updates(map[string]any{"canonical_name": name, "normalized_name": normalizeEntityName(name), "embedding_needs_rebuild": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		entity.CanonicalName, entity.NormalizedName, entity.EmbeddingNeedsRebuild = name, normalizeEntityName(name), true
		if err := notifyEntityProjection(tx, spaceID, entity.ID); err != nil {
			return err
		}
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "entity_renamed")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = s.refreshArticleHeadersForEntity(ctx, spaceID, entityID); err != nil {
		return &entity, err
	}
	return &entity, nil
}

func entityDatabaseInTransaction(db *gorm.DB) bool {
	if db == nil || db.Statement == nil {
		return false
	}
	_, ok := db.Statement.ConnPool.(gorm.TxCommitter)
	return ok
}

func (s *EntityService) UpdateEntityDescription(ctx context.Context, entityID, description string) (*types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated entity editor required")
	}
	description = strings.TrimSpace(description)
	if len([]rune(description)) > 20000 {
		return nil, werrors.NewBadRequestError("entity description exceeds 20000 characters")
	}
	var entity types.Entity
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, spaceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=? AND merged_into_id=''", entityID, spaceID).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		if err := requireExtractedEntityWriter(tx, entity.LibraryID); err != nil {
			return err
		}
		if err := tx.Model(&entity).Updates(map[string]any{"description": description, "embedding_needs_rebuild": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		entity.Description = description
		entity.EmbeddingNeedsRebuild = true
		if s.nameEmbeddingRebuilder != nil {
			return s.nameEmbeddingRebuilder.EnqueueTx(ctx, tx, spaceID, "entity_description_updated")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &entity, nil
}

func saveEntityAlias(tx *gorm.DB, entity types.Entity, name, method string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var collision types.EntityAlias
	var library types.Library
	if err := tx.Select("entity_mode").First(&library, "id=? AND space_id=?", entity.LibraryID, entity.SpaceID).Error; err != nil {
		return err
	}
	query := tx.Table("entity_aliases a").Select("a.*").Joins("JOIN entities e ON e.id=a.entity_id").Where("a.space_id=? AND a.library_id=? AND a.normalized_alias=?", entity.SpaceID, entity.LibraryID, normalizeEntityName(name))
	if library.ManagesEntities() {
		query = query.Where("e.archived_at IS NULL AND e.merged_into_id=''")
		if entity.ArchivedAt != nil {
			query = query.Where("e.id=?", entity.ID)
		}
	} else {
		query = query.Where("a.entity_type=?", entity.EntityType)
	}
	if err := query.First(&collision).Error; err == nil {
		if collision.EntityID != entity.ID {
			return werrors.NewBadRequestError("merge alias conflicts with another entity")
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	alias := types.EntityAlias{ID: uuid.NewString(), SpaceID: entity.SpaceID, LibraryID: entity.LibraryID, EntityType: entity.EntityType, EntityID: entity.ID, Alias: name, NormalizedAlias: normalizeEntityName(name), ResolutionMethod: method, CreatedAt: time.Now()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "library_id"}, {Name: "entity_id"}, {Name: "normalized_alias"}}, DoNothing: true}).Create(&alias).Error
}
