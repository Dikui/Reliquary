package service

import (
	"context"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ArticleContentTypeCorrector interface {
	CorrectContentType(context.Context, string, string) (*types.Entry, error)
}

// CorrectContentType changes the label while preserving the full article.
func (s *entryService) CorrectContentType(ctx context.Context, id, contentType string) (*types.Entry, error) {
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated editor required")
	}
	if s.db == nil {
		return nil, werrors.NewInternalServerError("content type corrections require PostgreSQL")
	}
	spaceID := types.MustSpaceIDFromContext(ctx)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var entry types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(&entry).Error; err != nil {
			return err
		}
		if !isIngestArticle(&entry) || entry.ParseStatus != types.ParseStatusCompleted {
			return werrors.NewBadRequestError("only completed articles can have their content type corrected")
		}
		var catalog []types.LibraryContentType
		if err := tx.Where("library_id=? AND enabled=true", entry.LibraryID).Find(&catalog).Error; err != nil {
			return err
		}
		valid := false
		for _, row := range catalog {
			if row.Name == contentType {
				valid = true
			}
		}
		if !valid {
			return werrors.NewBadRequestError("content type must belong to this library")
		}
		before := entry.ContentType
		if err := tx.Model(&entry).Updates(map[string]any{"content_type": contentType, "content_type_corrected": true, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "correct_content_type", id, map[string]any{"content_type": before}, map[string]any{"content_type": contentType})
	})
	if err != nil {
		return nil, err
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	return s.GetEntryByID(ctx, id)
}

func (s *entryService) contentTypeTransactionService(tx *gorm.DB) *entryService {
	copy := *s
	copy.db = tx
	copy.repo = repository.NewEntryRepository(tx)
	copy.chunkRepo = repository.NewChunkRepository(tx)
	copy.spaceRepo = repository.NewSpaceRepository(tx)
	copy.retrieveEngine = retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))
	copy.chunkService = NewChunkService(copy.chunkRepo, copy.repo, repository.NewLibraryRepository(tx), copy.modelService, copy.retrieveEngine)
	copy.vault, copy.resourceCatalog = nil, nil
	return &copy
}
