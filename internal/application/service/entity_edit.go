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
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *EntityService) MergeEntities(ctx context.Context, keepEntityID, mergeEntityID string) error {
	tenantID, err := s.requireTenant(ctx)
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
		if err := lockEntitySpace(tx, tenantID); err != nil {
			return err
		}
		var keep, merged types.Entity
		targets := []struct {
			id     string
			target *types.Entity
		}{{keepEntityID, &keep}, {mergeEntityID, &merged}}
		sort.Slice(targets, func(i, j int) bool { return targets[i].id < targets[j].id })
		for _, item := range targets {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", item.id, tenantID).First(item.target).Error; err != nil {
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
		if err := tx.Where("tenant_id = ? AND entity_id = ?", tenantID, merged.ID).Find(&aliases).Error; err != nil {
			return err
		}
		for _, alias := range aliases {
			var collision types.EntityAlias
			err := tx.Where("tenant_id = ? AND entity_type = ? AND normalized_alias = ? AND entity_id <> ?", tenantID, keep.EntityType, alias.NormalizedAlias, merged.ID).First(&collision).Error
			if err == nil {
				if collision.EntityID != keep.ID {
					return werrors.NewBadRequestError("merge alias conflicts with another space entity")
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
		if err := tx.Exec(`INSERT INTO knowledge_entities (tenant_id, knowledge_id, entity_id, created_at)
            SELECT tenant_id, knowledge_id, ?, created_at FROM knowledge_entities WHERE tenant_id=? AND entity_id=?
            ON CONFLICT (knowledge_id, entity_id) DO NOTHING`, keep.ID, tenantID, merged.ID).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND entity_id=?", tenantID, merged.ID).Delete(&types.KnowledgeEntity{}).Error; err != nil {
			return err
		}
		if err := saveEntityAlias(tx, keep, merged.CanonicalName, "merge"); err != nil {
			return err
		}
		actor, _ := types.PrincipalFromContext(ctx)
		record := types.EntityMergeRecord{ID: uuid.NewString(), TenantID: tenantID, KeepEntityID: keep.ID, MergeEntityID: merged.ID, Actor: actor.StorageID(), CreatedAt: time.Now()}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&merged).Update("merged_into_id", keep.ID).Error; err != nil {
			return err
		}
		if err := tx.Model(&keep).Updates(map[string]any{"embedding_needs_rebuild": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.refreshArticleHeadersForEntity(ctx, tenantID, keepEntityID); err != nil {
		return err
	}
	if s.nameEmbeddingRebuilder != nil && !entityDatabaseInTransaction(s.db) {
		return s.nameEmbeddingRebuilder.Enqueue(ctx, tenantID, "entity_merge")
	}
	return nil
}

func (s *EntityService) refreshArticleHeadersForEntity(ctx context.Context, tenantID uint64, entityID string) error {
	if s.headerRefresher == nil || entityDatabaseInTransaction(s.db) {
		return nil
	}
	var ids []string
	if err := s.db.WithContext(ctx).Model(&types.KnowledgeEntity{}).
		Where("tenant_id=? AND entity_id=?", tenantID, entityID).Pluck("knowledge_id", &ids).Error; err != nil {
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
	tenantID, err := s.requireTenant(ctx)
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
		if err := lockEntitySpace(tx, tenantID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND merged_into_id=''", entityID, tenantID).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		var collisions int64
		if err := tx.Model(&types.Entity{}).Where("tenant_id=? AND entity_type=? AND normalized_name=? AND id<>? AND merged_into_id=''", tenantID, entity.EntityType, normalizeEntityName(name), entity.ID).Count(&collisions).Error; err != nil {
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = s.refreshArticleHeadersForEntity(ctx, tenantID, entityID); err != nil {
		return &entity, err
	}
	if s.nameEmbeddingRebuilder != nil && !entityDatabaseInTransaction(s.db) {
		if err = s.nameEmbeddingRebuilder.Enqueue(ctx, tenantID, "entity_renamed"); err != nil {
			return &entity, err
		}
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
	tenantID, err := s.requireTenant(ctx)
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
		if err := lockEntitySpace(tx, tenantID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND merged_into_id=''", entityID, tenantID).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("entity not found")
		} else if err != nil {
			return err
		}
		if err := tx.Model(&entity).Updates(map[string]any{"description": description, "embedding_needs_rebuild": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		entity.Description = description
		entity.EmbeddingNeedsRebuild = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.nameEmbeddingRebuilder != nil && !entityDatabaseInTransaction(s.db) {
		if err := s.nameEmbeddingRebuilder.Enqueue(ctx, tenantID, "entity_description_updated"); err != nil {
			return &entity, err
		}
	}
	return &entity, nil
}

func saveEntityAlias(tx *gorm.DB, entity types.Entity, name, method string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var collision types.EntityAlias
	if err := tx.Where("tenant_id=? AND entity_type=? AND normalized_alias=?", entity.TenantID, entity.EntityType, normalizeEntityName(name)).First(&collision).Error; err == nil {
		if collision.EntityID != entity.ID {
			return werrors.NewBadRequestError("merge alias conflicts with another entity")
		}
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	alias := types.EntityAlias{ID: uuid.NewString(), TenantID: entity.TenantID, EntityType: entity.EntityType, EntityID: entity.ID, Alias: name, NormalizedAlias: normalizeEntityName(name), ResolutionMethod: method, CreatedAt: time.Now()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "entity_type"}, {Name: "normalized_alias"}}, DoNothing: true}).Create(&alias).Error
}
