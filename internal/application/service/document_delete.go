package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// DeleteDocument removes an imported personal document by its explicit ID.
func (s *entryService) DeleteDocument(ctx context.Context, id string) error {
	ctx, _, _, err := s.ingestIdentity(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return werrors.NewBadRequestError("document_id is required")
	}
	document, err := s.GetEntryByID(ctx, id)
	if err != nil {
		return err
	}
	if document.SourceKind != "mine" {
		return werrors.NewBadRequestError("只能删除本人文档")
	}
	if _, ok := ctx.Value(types.SpaceInfoContextKey).(*types.Space); !ok {
		space, err := s.spaceRepo.GetSpaceByID(ctx, document.SpaceID)
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	}
	return s.DeleteEntry(ctx, id)
}

func (s *entryService) removeDocumentRecords(ctx context.Context, document *types.Entry) error {
	if s.db == nil {
		return s.repo.HardDeleteEntry(ctx, document.SpaceID, document.ID)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("entry_id=?", document.ID).Delete(&types.EntryProcessingSpan{}).Error; err != nil {
			return err
		}
		if err := tx.Table("embeddings").Where("entry_id=?", document.ID).Delete(nil).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("space_id=? AND entry_id=?", document.SpaceID, document.ID).Delete(&types.Chunk{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("space_id=? AND id=?", document.SpaceID, document.ID).Delete(&types.Entry{}).Error
	})
}
