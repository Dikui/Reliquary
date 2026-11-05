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

// EntityService owns library-scoped canonical identities and article mentions.
type EntityService struct {
	files                  interfaces.FileService
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
	entities, _, err := s.resolveEntityCandidates(ctx, entryID, input)
	return entities, err
}

func (s *EntityService) resolveEntityCandidates(ctx context.Context, entryID string, input []types.EntityCandidate) ([]types.Entity, map[string]string, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, nil, err
	}
	var article types.Entry
	if err = s.db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, entryID).First(&article).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, werrors.NewNotFoundError("article not found")
	} else if err != nil {
		return nil, nil, err
	}
	if len(input) > 500 {
		return nil, nil, werrors.NewBadRequestError("at most 500 entity candidates")
	}
	candidates := make([]entityResolutionInput, 0, len(input))
	for _, item := range input {
		if strings.TrimSpace(item.Name) == "" {
			return nil, nil, werrors.NewBadRequestError("entity name is required")
		}
		if len([]rune(item.Name)) > 512 {
			return nil, nil, werrors.NewBadRequestError("entity name exceeds 512 characters")
		}
		for _, alias := range item.Aliases {
			if len([]rune(alias)) > 512 {
				return nil, nil, werrors.NewBadRequestError("entity alias exceeds 512 characters")
			}
		}
		candidates = append(candidates, entityResolutionInput{Mention: item.Name, Canonical: item.Name, EntityType: types.NormalizeEntityType(item.EntityType), Aliases: item.Aliases, Description: item.Description})
	}
	if len(candidates) == 0 {
		return []types.Entity{}, map[string]string{}, nil
	}
	var library types.Library
	if err = s.db.WithContext(ctx).Where("id=? AND space_id=?", article.LibraryID, spaceID).First(&library).Error; err != nil {
		return nil, nil, err
	}
	if library.ManagesEntities() {
		return s.resolveManagedEntityCandidates(ctx, &article, candidates)
	}
	if s.models == nil {
		return nil, nil, errors.New("entity judgment model service is unavailable")
	}
	modelID, err := ResolveModelTier(ctx, s.db, &library, types.ModelTierEntityResolution)
	if err != nil {
		return nil, nil, err
	}
	if modelID == "" {
		return nil, nil, errors.New("entity judgment model is not configured")
	}
	model, err := s.models.GetChatModel(types.WithModelTier(ctx, types.ModelTierEntityResolution), modelID)
	if err != nil {
		return nil, nil, err
	}
	planned, mentions, err := s.resolveEntities(types.WithModelTier(ctx, types.ModelTierEntityResolution), model, spaceID, article.LibraryID, entryID, candidates)
	if err != nil {
		return nil, nil, err
	}
	resolvedIDs := []string{}
	resolvedMentions := map[string]string{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpaceShared(tx, spaceID); err != nil {
			return err
		}
		// Recheck the article while writing so a concurrent deletion cannot leave mentions.
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id=? AND space_id=? AND library_id=?", entryID, spaceID, article.LibraryID).First(&current).Error; err != nil {
			return err
		}
		ids, err := persistEntities(tx, planned)
		if err != nil {
			return err
		}
		for key, entity := range mentions {
			resolvedMentions[key] = ids[entity.ID]
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
		return nil, nil, err
	}
	entities := []types.Entity{}
	if err = s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, resolvedIDs).Order("canonical_name,id").Find(&entities).Error; err != nil {
		return nil, nil, err
	}
	if err := s.loadAliases(ctx, spaceID, entities); err != nil {
		return nil, nil, err
	}
	return entities, resolvedMentions, nil
}

func (s *EntityService) resolveLibrary(ctx context.Context, libraryID string) (uint64, string, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return 0, "", err
	}

	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return 0, "", werrors.NewBadRequestError("library_id is required")
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, libraryID).First(&library).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, "", werrors.NewNotFoundError("library not found")
	} else if err != nil {
		return 0, "", err
	}
	return spaceID, libraryID, nil
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
	spaceID, libraryID, err := s.resolveLibrary(ctx, q.LibraryID)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Model(&types.Entity{}).Where("entities.space_id=? AND entities.merged_into_id=''", spaceID)
	query = query.Where("entities.library_id=?", libraryID)
	if !q.IncludeArchived {
		query = query.Where("entities.archived_at IS NULL")
	}
	if q.Kind != "" {
		query = query.Where("entities.kind=?", q.Kind)
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
	if err := s.loadAliases(ctx, spaceID, entities); err != nil {
		return nil, err
	}
	pointers := make([]*types.Entity, len(entities))
	for i := range entities {
		pointers[i] = &entities[i]
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, pointers, time.Now()); err != nil {
		return nil, err
	}
	return entities, nil
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
	if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{&entities[0]}, time.Now()); err != nil {
		return nil, err
	}
	result := &types.EntityDetail{Entity: entities[0], Entries: []types.Entry{}, LibraryGroups: []types.EntityLibraryGroup{}}
	err = s.db.WithContext(ctx).Model(&types.Entry{}).Joins("JOIN entry_entities links ON links.entry_id=entries.id AND links.space_id=entries.space_id").Where("entries.space_id=? AND links.entity_id=? AND links.excluded=false", spaceID, entity.ID).Order("entries.created_at DESC,entries.id").Find(&result.Entries).Error
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

// Match under the identity barrier so archived or renamed entities cannot be
// associated from a stale candidate catalog. Managed identities are never written.
func (s *EntityService) resolveManagedEntityCandidates(ctx context.Context, article *types.Entry, input []entityResolutionInput) ([]types.Entity, map[string]string, error) {
	resolved := []types.Entity{}
	mentions := map[string]string{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpaceShared(tx, article.SpaceID); err != nil {
			return err
		}
		var library types.Library
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", article.LibraryID, article.SpaceID).First(&library).Error; err != nil {
			return err
		}
		if !library.ManagesEntities() {
			return werrors.NewConflictError("library entity mode changed during processing")
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=? AND library_id=?", article.ID, article.SpaceID, article.LibraryID).First(&current).Error; err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, candidate := range input {
			names := []string{normalizeEntityName(candidate.Mention), normalizeEntityName(candidate.Canonical)}
			for _, alias := range candidate.Aliases {
				names = append(names, normalizeEntityName(alias))
			}
			entity, err := findManagedEntityInLibraries(tx, article.SpaceID, appendUniqueEntityString(append([]string{}, library.ProductLibraryIDs...), library.ID), names)
			if err != nil {
				return err
			}
			if entity == nil {
				continue
			}
			var link types.EntryEntity
			err = tx.Where("entry_id=? AND entity_id=?", article.ID, entity.ID).First(&link).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && link.Excluded {
				continue
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				link = types.EntryEntity{SpaceID: article.SpaceID, EntryID: article.ID, EntityID: entity.ID, Origin: "extracted", Evidence: []types.SourceLocation{}, CreatedAt: time.Now()}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&link).Error; err != nil {
					return err
				}
			}
			mentions[entityNameKey(candidate.EntityType, candidate.Mention)] = entity.ID
			if !seen[entity.ID] {
				resolved = append(resolved, *entity)
				seen[entity.ID] = true
			}
		}
		return nil
	})
	return resolved, mentions, err
}

// Callers hold the entity barrier while matching and storing links.
func findManagedEntity(tx *gorm.DB, spaceID uint64, libraryID string, names []string) (*types.Entity, error) {
	return findManagedEntityInLibraries(tx, spaceID, []string{libraryID}, names)
}

func findManagedEntityInLibraries(tx *gorm.DB, spaceID uint64, libraryIDs []string, names []string) (*types.Entity, error) {
	var matches []types.Entity
	if err := tx.Where("space_id=? AND library_id IN ? AND merged_into_id='' AND archived_at IS NULL", spaceID, libraryIDs).
		Where("normalized_name IN ? OR lower(code) IN ? OR id IN (SELECT entity_id FROM entity_aliases WHERE space_id=? AND library_id IN ? AND normalized_alias IN ?)", names, names, spaceID, libraryIDs, names).
		Limit(2).Find(&matches).Error; err != nil {
		return nil, err
	}
	if len(matches) != 1 {
		return nil, nil
	}
	return &matches[0], nil
}
