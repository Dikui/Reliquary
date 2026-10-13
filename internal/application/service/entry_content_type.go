package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// ArticleContentTypeCorrector is a separate management capability so ingestion
// clients and their test substitutes do not need unrelated mutation methods.
type ArticleContentTypeCorrector interface {
	CorrectContentType(context.Context, string, string, bool) (*types.Entry, error)
}

// CorrectContentType performs a user correction. Cross-mode conversions finish
// synchronously so a failed fetch or model/index call leaves the old article
// available with its original classification, summary, body and indexes.
func (s *entryService) CorrectContentType(ctx context.Context, id, contentType string, confirmRemoveBody bool) (*types.Entry, error) {
	return s.correctContentType(ctx, id, contentType, confirmRemoveBody, "")
}

func (s *entryService) correctContentType(ctx context.Context, id, contentType string, confirmRemoveBody bool, reviewAction string) (*types.Entry, error) {
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated editor required")
	}
	if reviewAction != "confirm" && (types.NormalizeContentType(contentType) != contentType || contentType == "") {
		return nil, werrors.NewBadRequestError("invalid content type")
	}
	if s.db == nil {
		return nil, errors.New("content type corrections require PostgreSQL")
	}
	if s.vault != nil {
		lock := s.vault.articleLock(id)
		lock.Lock()
		defer lock.Unlock()
	}
	spaceID := types.MustSpaceIDFromContext(ctx)
	before, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	if !isIngestArticle(before) || before.ParseStatus != types.ParseStatusCompleted {
		return nil, werrors.NewBadRequestError("only completed articles can have their content type corrected")
	}
	if reviewAction != "" && before.ProcessingReviewStatus != types.ProcessingReviewPending {
		return nil, werrors.NewBadRequestError("the article is not pending processing review")
	}
	if reviewAction == "confirm" {
		contentType = before.ProcessingReviewContentType
		if types.NormalizeContentType(contentType) != contentType || contentType == "" {
			return nil, werrors.NewBadRequestError("the model type cannot be confirmed; choose a valid content type")
		}
	}
	mode := types.ProcessingModeForContentType(contentType)
	if before.SourceUnverified || before.SourceKind == "mine" {
		mode = types.ProcessingModeFull
	}
	if before.ProcessingMode == types.ProcessingModeFull && mode == types.ProcessingModeSummaryOnly && !confirmRemoveBody {
		return nil, werrors.NewBadRequestError("confirm_remove_body is required because the article body, body vectors and attachments will be removed")
	}
	if reviewAction == "" && before.ContentType == contentType && before.ContentTypeCorrected && before.ProcessingReviewStatus != types.ProcessingReviewPending {
		return s.GetEntryByID(ctx, id)
	}
	library, err := s.libraryService.GetLibraryByID(ctx, before.LibraryID)
	if err != nil {
		return nil, err
	}
	corrected := reviewAction != "confirm"
	missingBody := before.ProcessingMode == types.ProcessingModeSummaryOnly && mode == types.ProcessingModeFull
	var updated types.Entry
	var fileChange *vaultContentTypeChange
	var discardedRefs []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(&updated).Error; err != nil {
			return err
		}
		if updated.ContentType != before.ContentType || updated.ProcessingMode != before.ProcessingMode || updated.ParseStatus != types.ParseStatusCompleted || updated.ProcessingReviewStatus != before.ProcessingReviewStatus || updated.SourceUnverified != before.SourceUnverified {
			return werrors.NewConflictError("the article changed during conversion; reload it and try again")
		}
		updated.ContentType, updated.ContentTypeCorrected, updated.ProcessingMode, updated.UpdatedAt = contentType, corrected, mode, time.Now()
		if missingBody {
			originalURL, err := prepareMissingContentOriginalURL(&updated)
			if err != nil {
				return err
			}
			missing := updated.IngestMissing()
			hasContent := false
			for i := range missing {
				if missing[i].Type == "content" {
					hasContent = true
					missing[i].Reason = "修正为全文保存，需要正文"
					if !validIngestURL(missing[i].SourceURL) {
						missing[i].SourceURL = originalURL
					}
				}
			}
			if !hasContent {
				missing = append(missing, types.IngestMissing{Type: "content", SourceURL: originalURL,
					Reason: "修正为全文保存，需要正文", APIKeyID: updated.IngestAPIKeyID, APIKeyName: updated.IngestAPIKeyName, SubmittedAt: time.Now()})
			}
			if err := updated.SetIngestMissing(missing); err != nil {
				return err
			}
			updated.Incomplete, updated.ExternalCompletionAttempted = true, false
		}
		if reviewAction == "confirm" {
			updated.ProcessingReviewStatus = types.ProcessingReviewConfirmed
		} else if before.ProcessingReviewStatus == types.ProcessingReviewPending || reviewAction == "correct" {
			updated.ProcessingReviewStatus = types.ProcessingReviewCorrected
		}
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		transactional := s.contentTypeTransactionService(tx)
		if before.ProcessingMode == types.ProcessingModeFull && mode == types.ProcessingModeSummaryOnly {
			discardedRefs, err = transactional.clearCorrectedArticleBody(ctx, &updated)
			if err != nil {
				return err
			}
		}
		if s.vault != nil && before.ProcessingMode != mode && !missingBody {
			read := func(ctx context.Context, ref string) (io.ReadCloser, error) {
				return s.resolveFileServiceForPath(ctx, library, ref).GetFile(ctx, ref)
			}
			fileChange, err = s.vault.changeCorrectedArticleBody(ctx, tx, &updated, "", read)
			if err != nil {
				return err
			}
		}
		operationType := "correct_content_type"
		if reviewAction != "" {
			operationType = "processing_review_" + reviewAction
		}
		return NewOperationLogService(s.db).RecordTx(ctx, tx, operationType, id,
			processingReviewLogValue(before), processingReviewLogValue(&updated))
	})
	if err != nil {
		if fileChange != nil {
			if rollbackErr := fileChange.rollback(); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("restore article file: %w", rollbackErr))
			}
		}
		if types.IsChatGPTDeferred(err) {
			return before, err
		}
		return before, werrors.NewBadRequestError("could not correct the article content type: " + err.Error())
	}
	if fileChange != nil {
		if err := fileChange.finish(); err != nil {
			s.vault.recordArticleError(ctx, id, err)
		}
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	s.deleteCorrectionFiles(ctx, library, discardedRefs, id)
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

func (s *entryService) clearCorrectedArticleBody(ctx context.Context, k *types.Entry) ([]string, error) {
	if err := s.db.WithContext(ctx).Table("embeddings").Where("library_id=? AND entry_id=?", k.LibraryID, k.ID).Delete(nil).Error; err != nil {
		return nil, err
	}
	if err := s.chunkService.DeleteChunksByEntryID(ctx, k.ID); err != nil {
		return nil, err
	}
	if k.StorageSize > 0 {
		if err := s.spaceRepo.AdjustStorageUsed(ctx, k.SpaceID, -k.StorageSize); err != nil {
			return nil, err
		}
	}
	metadata := map[string]json.RawMessage{}
	if err := json.Unmarshal(k.Metadata, &metadata); err != nil && len(k.Metadata) > 0 {
		return nil, err
	}
	refs := []string{k.FilePath}
	var path string
	var oldImages []string
	_ = json.Unmarshal(metadata["processed_body_path"], &path)
	_ = json.Unmarshal(metadata["processed_images"], &oldImages)
	refs = append(append(refs, path), oldImages...)
	if intake, err := k.IntakeMetadata(); err != nil {
		return nil, err
	} else if intake != nil {
		refs = append(append(refs, intake.OriginalPath), intake.Images...)
		intake.OriginalPath, intake.Images = "", nil
		metadata["intake"], _ = json.Marshal(intake)
	}
	delete(metadata, "processed_body_path")
	delete(metadata, "processed_images")
	delete(metadata, "content")
	delete(metadata, "chunks")
	k.Metadata, _ = json.Marshal(metadata)
	k.FilePath, k.VaultBody, k.VaultOriginalPath, k.EmbeddingModelID, k.StorageSize = "", "", "", "", 0
	return refs, s.repo.UpdateEntry(ctx, k)
}

func (s *entryService) deleteCorrectionFiles(ctx context.Context, library *types.Library, refs []string, ownerIDs ...string) {
	seen := map[string]bool{}
	releaser := entryResourceOwners(s.resourceCatalog, ownerIDs...)
	for _, ref := range refs {
		if ref == "" || seen[ref] || strings.HasPrefix(ref, "http:") || strings.HasPrefix(ref, "https:") || strings.HasPrefix(ref, "data:") || !strings.Contains(ref, "://") {
			continue
		}
		seen[ref] = true
		if !releaser.deletable(ctx, ref) {
			continue
		}
		if err := s.resolveFileServiceForPath(ctx, library, ref).DeleteFile(ctx, ref); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warnf(ctx, "Remove content type conversion copy %s: %v", ref, err)
		}
	}
}
