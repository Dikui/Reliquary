package service

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	werrors "reliquary/internal/errors"

	"gorm.io/gorm"
)

// EntityService owns canonical identities and article mentions in the personal space.
type EntityService struct {
	db              *gorm.DB
	headerRefresher ArticleHeaderRefresher
}

// NewEntityService constructs the maintained entity directory.
func NewEntityService(db *gorm.DB, _ interfaces.ModelService) *EntityService {
	return &EntityService{db: db}
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
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&types.Entity{}).Where("entities.space_id=? AND entities.merged_into_id=''", spaceID)
	if q.EntityType != "" {
		if types.NormalizeEntityType(q.EntityType) == "" {
			return nil, werrors.NewBadRequestError("invalid entity type")
		}
		query = query.Where("entities.entity_type=?", types.NormalizeEntityType(q.EntityType))
	}
	if name := types.NormalizeEntityKey(q.Entity); name != "" {
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
	result := &types.EntityDetail{Entity: entities[0], Entries: []types.Entry{}, LibraryGroups: []types.EntityLibraryGroup{}}
	err = s.db.WithContext(ctx).Model(&types.Entry{}).Joins("JOIN entry_entities links ON links.entry_id=entries.id AND links.space_id=entries.space_id").Where("entries.space_id=? AND links.entity_id=?", spaceID, entity.ID).Order("entries.created_at DESC,entries.id").Find(&result.Entries).Error
	if err == nil {
		entries := make([]*types.Entry, len(result.Entries))
		for i := range result.Entries {
			entries[i] = &result.Entries[i]
		}
		err = populateEntryLibraryNames(ctx, s.db, entries)
		positions := map[string]int{}
		for _, entry := range result.Entries {
			i, ok := positions[entry.LibraryID]
			if !ok {
				i = len(result.LibraryGroups)
				positions[entry.LibraryID] = i
				result.LibraryGroups = append(result.LibraryGroups, types.EntityLibraryGroup{LibraryID: entry.LibraryID, LibraryName: entry.LibraryName, Entries: []types.Entry{}})
			}
			group := &result.LibraryGroups[i]
			group.Entries = append(group.Entries, entry)
			group.Count++
		}
	}
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
