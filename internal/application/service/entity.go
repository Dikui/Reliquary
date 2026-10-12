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
	headerRefresher        ArticleHeaderRefresher
}

func NewEntityService(db *gorm.DB, models interfaces.ModelService) *EntityService {
	return &EntityService{db: db, models: models}
}
func (s *EntityService) SetNameEmbeddingRebuilder(r *EntityNameEmbeddingRebuilder) {
	s.nameEmbeddingRebuilder = r
}
func (s *EntityService) SetHeaderRefresher(r ArticleHeaderRefresher) {
	s.headerRefresher = r
}
func (s *EntityService) requireSpace(ctx context.Context) (uint64, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, werrors.NewUnauthorizedError("personal space is required")
	}
	return id, nil
}

// ResolveEntities resolves caller-provided candidates and records article associations.
// It is intentionally independent from article intake.
func (s *EntityService) ResolveEntities(ctx context.Context, entryID string, input []types.EntityCandidate) ([]types.Entity, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var article types.Entry
	if err = s.db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, entryID).First(&article).Error; errors.Is(err, gorm.ErrRecordNotFound) {
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
	if err = s.db.WithContext(ctx).Where("id=? AND space_id=?", article.KnowledgeBaseID, spaceID).First(&kb).Error; err != nil {
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
	planned, _, err := s.resolveEntities(types.WithModelTier(ctx, types.ModelTierEntityResolution), model, spaceID, entryID, candidates)
	if err != nil {
		return nil, err
	}
	resolvedIDs := []string{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Recheck the article while writing so a concurrent deletion cannot leave mentions.
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id=? AND space_id=?", entryID, spaceID).First(&current).Error; err != nil {
			return err
		}
		ids, err := persistEntities(tx, planned)
		if err != nil {
			return err
		}
		for _, entity := range planned {
			id := ids[entity.ID]
			resolvedIDs = appendUniqueEntityString(resolvedIDs, id)
			link := types.EntryEntity{SpaceID: spaceID, EntryID: entryID, EntityID: id, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "entity_id"}}, DoNothing: true}).Create(&link).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	entities := []types.Entity{}
	if err = s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, resolvedIDs).Order("canonical_name,id").Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, s.loadAliases(ctx, spaceID, entities)
}

func (s *EntityService) resolveKnowledgeBase(ctx context.Context) (uint64, string, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return 0, "", err
	}

	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Select("id").Where("space_id=?", spaceID).First(&kb).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", werrors.NewNotFoundError("knowledge base not found")
	} else if err != nil {
		return 0, "", err
	}
	return spaceID, kb.ID, nil
}
func entityQueryLimit(limit int) int {
	if limit < 1 {
		return 50
	}
	return min(limit, 100)
}
func (s *EntityService) loadAliases(ctx context.Context, spaceID uint64, entities []types.Entity) error {
	ids := []string{}
	for _, entity := range entities {
		ids = append(ids, entity.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var aliases []types.EntityAlias
	if err := s.db.WithContext(ctx).Where("space_id=? AND entity_id IN ?", spaceID, ids).Order("alias,id").Find(&aliases).Error; err != nil {
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
	spaceID, kbID, err := s.resolveKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&types.Entity{}).Where("entities.space_id=? AND entities.merged_into_id=''", spaceID)
	if kbID != "" {
		query = query.Where(`EXISTS (SELECT 1 FROM entry_entities links JOIN entries k ON k.id=links.entry_id AND k.space_id=links.space_id WHERE links.space_id=entities.space_id AND links.entity_id=entities.id AND k.knowledge_base_id=? AND k.deleted_at IS NULL)`, kbID)
	}
	if q.EntityType != "" {
		query = query.Where("entities.entity_type=?", types.NormalizeEntityType(q.EntityType))
	}
	if name := normalizeEntityName(q.Entity); name != "" {
		pattern := "%" + name + "%"
		query = query.Where(`(entities.normalized_name LIKE ? OR EXISTS (SELECT 1 FROM entity_aliases a WHERE a.space_id=entities.space_id AND a.entity_id=entities.id AND a.normalized_alias LIKE ?))`, pattern, pattern)
	}
	entities := []types.Entity{}
	if err := query.Order("entities.canonical_name,entities.id").Limit(entityQueryLimit(q.Limit)).Offset(max(q.Offset, 0)).Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, s.loadAliases(ctx, spaceID, entities)
}
func (s *EntityService) EntityDetail(ctx context.Context, id string) (*types.EntityDetail, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, id).First(&entity).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, werrors.NewNotFoundError("entity not found")
	} else if err != nil {
		return nil, err
	}
	if entity.MergedIntoID != "" {
		return s.EntityDetail(ctx, entity.MergedIntoID)
	}
	entities := []types.Entity{entity}
	if err := s.loadAliases(ctx, spaceID, entities); err != nil {
		return nil, err
	}
	result := &types.EntityDetail{Entity: entities[0], Entries: []types.Entry{}}
	err = s.db.WithContext(ctx).Model(&types.Entry{}).Joins("JOIN entry_entities links ON links.entry_id=entries.id AND links.space_id=entries.space_id").Where("entries.space_id=? AND links.entity_id=?", spaceID, entity.ID).Order("entries.created_at DESC,entries.id").Find(&result.Entries).Error
	return result, err
}
func (s *EntityService) ListMergeRecords(ctx context.Context, id string) ([]types.EntityMergeRecord, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	items := []types.EntityMergeRecord{}
	err = s.db.WithContext(ctx).Where("space_id=? AND (keep_entity_id=? OR merge_entity_id=?)", spaceID, id, id).Order("created_at DESC,id").Find(&items).Error
	return items, err
}
