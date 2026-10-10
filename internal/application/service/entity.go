package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// EntityService owns canonical identities and article mentions in the personal space.
type EntityService struct {
	db                     *gorm.DB
	models                 interfaces.ModelService
	nameEmbeddingRebuilder *EntityNameEmbeddingRebuilder
}

func NewEntityService(db *gorm.DB, models interfaces.ModelService) *EntityService {
	return &EntityService{db: db, models: models}
}
func (s *EntityService) SetNameEmbeddingRebuilder(r *EntityNameEmbeddingRebuilder) {
	s.nameEmbeddingRebuilder = r
}
func (s *EntityService) requireTenant(ctx context.Context) (uint64, error) {
	id, ok := types.TenantIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

// ResolveEntities resolves caller-provided candidates and records article associations.
// It is intentionally independent from article intake.
func (s *EntityService) ResolveEntities(ctx context.Context, knowledgeID string, input []types.EntityCandidate) ([]types.Entity, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	var article types.Knowledge
	if err = s.db.WithContext(ctx).Where("tenant_id=? AND id=?", tenantID, knowledgeID).First(&article).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, werrors.NewNotFoundError("article not found")
	} else if err != nil {
		return nil, err
	}
	if len(input) > 500 {
		return nil, werrors.NewBadRequestError("at most 500 entity candidates")
	}
	candidates := make([]entityResolutionInput, 0, len(input))
	for _, item := range input {
		if strings.TrimSpace(item.Name) == "" {
			return nil, werrors.NewBadRequestError("entity name is required")
		}
		if len([]rune(item.Name)) > 512 {
			return nil, werrors.NewBadRequestError("entity name exceeds 512 characters")
		}
		for _, alias := range item.Aliases {
			if len([]rune(alias)) > 512 {
				return nil, werrors.NewBadRequestError("entity alias exceeds 512 characters")
			}
		}
		candidates = append(candidates, entityResolutionInput{Mention: item.Name, Canonical: item.Name, EntityType: types.NormalizeEntityType(item.EntityType), Aliases: item.Aliases, Description: item.Description})
	}
	if len(candidates) == 0 {
		return []types.Entity{}, nil
	}
	if s.models == nil {
		return nil, errors.New("entity judgment model service is unavailable")
	}
	var kb types.KnowledgeBase
	if err = s.db.WithContext(ctx).Where("id=? AND tenant_id=?", article.KnowledgeBaseID, tenantID).First(&kb).Error; err != nil {
		return nil, err
	}
	modelID, err := ResolveModelTier(ctx, s.db, &kb, types.ModelTierEntityResolution)
	if err != nil {
		return nil, err
	}
	if modelID == "" {
		return nil, errors.New("entity judgment model is not configured")
	}
	model, err := s.models.GetChatModel(types.WithModelTier(ctx, types.ModelTierEntityResolution), modelID)
	if err != nil {
		return nil, err
	}
	planned, _, err := s.resolveEntities(types.WithModelTier(ctx, types.ModelTierEntityResolution), model, tenantID, knowledgeID, candidates)
	if err != nil {
		return nil, err
	}
	resolvedIDs := []string{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Recheck the article while writing so a concurrent deletion cannot leave mentions.
		var current types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id=? AND tenant_id=?", knowledgeID, tenantID).First(&current).Error; err != nil {
			return err
		}
		ids, err := persistEntities(tx, planned)
		if err != nil {
			return err
		}
		for _, entity := range planned {
			id := ids[entity.ID]
			resolvedIDs = appendUniqueEntityString(resolvedIDs, id)
			link := types.KnowledgeEntity{TenantID: tenantID, KnowledgeID: knowledgeID, EntityID: id, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "knowledge_id"}, {Name: "entity_id"}}, DoNothing: true}).Create(&link).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	entities := []types.Entity{}
	if err = s.db.WithContext(ctx).Where("tenant_id=? AND id IN ?", tenantID, resolvedIDs).Order("canonical_name,id").Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, s.loadAliases(ctx, tenantID, entities)
}

func (s *EntityService) resolveKnowledgeBase(ctx context.Context) (uint64, string, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return 0, "", err
	}

	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Select("id").Where("tenant_id=?", tenantID).First(&kb).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", werrors.NewNotFoundError("knowledge base not found")
	} else if err != nil {
		return 0, "", err
	}
	return tenantID, kb.ID, nil
}
func entityQueryLimit(limit int) int {
	if limit < 1 {
		return 50
	}
	return min(limit, 100)
}
func (s *EntityService) loadAliases(ctx context.Context, tenantID uint64, entities []types.Entity) error {
	ids := []string{}
	for _, entity := range entities {
		ids = append(ids, entity.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var aliases []types.EntityAlias
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND entity_id IN ?", tenantID, ids).Order("alias,id").Find(&aliases).Error; err != nil {
		return err
	}
	for i := range entities {
		entities[i].Aliases = []string{}
		for _, alias := range aliases {
			if alias.EntityID == entities[i].ID {
				entities[i].Aliases = appendUniqueEntityString(entities[i].Aliases, alias.Alias)
			}
		}
	}
	return nil
}
func (s *EntityService) ListEntities(ctx context.Context, q types.EntityQuery) ([]types.Entity, error) {
	tenantID, kbID, err := s.resolveKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&types.Entity{}).Where("entities.tenant_id=? AND entities.merged_into_id=''", tenantID)
	if kbID != "" {
		query = query.Where(`EXISTS (SELECT 1 FROM knowledge_entities links JOIN knowledges k ON k.id=links.knowledge_id AND k.tenant_id=links.tenant_id WHERE links.tenant_id=entities.tenant_id AND links.entity_id=entities.id AND k.knowledge_base_id=? AND k.deleted_at IS NULL)`, kbID)
	}
	if q.EntityType != "" {
		query = query.Where("entities.entity_type=?", types.NormalizeEntityType(q.EntityType))
	}
	if name := normalizeEntityName(q.Entity); name != "" {
		pattern := "%" + name + "%"
		query = query.Where(`(entities.normalized_name LIKE ? OR EXISTS (SELECT 1 FROM entity_aliases a WHERE a.tenant_id=entities.tenant_id AND a.entity_id=entities.id AND a.normalized_alias LIKE ?))`, pattern, pattern)
	}
	entities := []types.Entity{}
	if err := query.Order("entities.canonical_name,entities.id").Limit(entityQueryLimit(q.Limit)).Offset(max(q.Offset, 0)).Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, s.loadAliases(ctx, tenantID, entities)
}
func (s *EntityService) EntityDetail(ctx context.Context, id string) (*types.EntityDetail, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=?", tenantID, id).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, werrors.NewNotFoundError("entity not found")
	} else if err != nil {
		return nil, err
	}
	if entity.MergedIntoID != "" {
		return s.EntityDetail(ctx, entity.MergedIntoID)
	}
	entities := []types.Entity{entity}
	if err := s.loadAliases(ctx, tenantID, entities); err != nil {
		return nil, err
	}
	result := &types.EntityDetail{Entity: entities[0], Knowledge: []types.Knowledge{}}
	err = s.db.WithContext(ctx).Model(&types.Knowledge{}).Joins("JOIN knowledge_entities links ON links.knowledge_id=knowledges.id AND links.tenant_id=knowledges.tenant_id").Where("knowledges.tenant_id=? AND links.entity_id=?", tenantID, entity.ID).Order("knowledges.created_at DESC,knowledges.id").Find(&result.Knowledge).Error
	return result, err
}
func (s *EntityService) ListMergeRecords(ctx context.Context, id string) ([]types.EntityMergeRecord, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	items := []types.EntityMergeRecord{}
	err = s.db.WithContext(ctx).Where("tenant_id=? AND (keep_entity_id=? OR merge_entity_id=?)", tenantID, id, id).Order("created_at DESC,id").Find(&items).Error
	return items, err
}
