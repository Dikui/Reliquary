package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"gorm.io/gorm"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *EntityService) readShelfSnapshot(ctx context.Context, read func(*EntityService) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := *s
		reader.db = tx
		return read(&reader)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

func (s *EntityService) shelfLibraryForRead(ctx context.Context, id string) (*types.Library, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	var library types.Library
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, spaceID).First(&library).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("shelf library not found")
		}
		return nil, err
	}
	if !library.ManagesEntities() || !library.ShelfEnabled {
		return nil, apperrors.NewBadRequestError("this operation requires an enabled shelf")
	}
	return &library, nil
}

func shelfPage(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return 0, apperrors.NewBadRequestError("limit must be 1-100 and offset must be nonnegative")
	}
	return limit, nil
}

// ShelfFind returns every matching identity, including cross-category ambiguity.
func (s *EntityService) ShelfFind(ctx context.Context, libraryID string, input types.ShelfFindRequest) (*types.ShelfFindResult, error) {
	var result *types.ShelfFindResult
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		var err error
		result, err = reader.shelfFind(ctx, libraryID, input)
		return err
	})
	return result, err
}

func (s *EntityService) shelfFind(ctx context.Context, libraryID string, input types.ShelfFindRequest) (*types.ShelfFindResult, error) {
	library, err := s.shelfLibraryForRead(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	limit, err := shelfPage(input.Limit, input.Offset)
	if err != nil {
		return nil, err
	}
	name := normalizeEntityName(input.Query)
	if name == "" || len([]rune(name)) > 512 {
		return nil, apperrors.NewBadRequestError("q requires 1-512 characters")
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(name) + "%"
	query := s.db.WithContext(ctx).Model(&types.Entity{}).Where("entities.library_id=? AND entities.space_id=? AND entities.archived_at IS NULL AND entities.merged_into_id=''", library.ID, library.SpaceID).
		Where(`(entities.normalized_name LIKE ? OR lower(entities.code) LIKE ? OR EXISTS (SELECT 1 FROM entity_aliases a WHERE a.space_id=entities.space_id AND a.library_id=entities.library_id AND a.entity_id=entities.id AND a.normalized_alias LIKE ?))`, pattern, pattern, pattern)
	result := &types.ShelfFindResult{Items: []types.Entity{}}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("entities.canonical_name,entities.kind,entities.id").Limit(limit).Offset(input.Offset).Find(&result.Items).Error; err != nil {
		return nil, err
	}
	if err := s.loadAliases(ctx, library.SpaceID, result.Items); err != nil {
		return nil, err
	}
	for i := range result.Items {
		result.Items[i].KindLabel = managedKindLabel(library, result.Items[i].Kind)
		// Find is an identity lookup. Facts are resolved with their evidence by get.
		result.Items[i].Attributes = nil
	}
	result.Truncated = int64(len(result.Items)) < result.Total
	return result, nil
}
