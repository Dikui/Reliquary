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
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

// ArticleContentTypeCorrector is a separate management capability so ingestion
// clients and their test substitutes do not need unrelated mutation methods.
type ArticleContentTypeCorrector interface {
	CorrectContentType(context.Context, string, string, bool) (*types.Knowledge, error)
}

// CorrectContentType performs a user correction. Cross-mode conversions finish
// synchronously so a failed fetch or model/index call leaves the old article
// available with its original classification, summary, body and indexes.
func (s *knowledgeService) CorrectContentType(ctx context.Context, id, contentType string, confirmRemoveBody bool) (*types.Knowledge, error) {
	return s.correctContentType(ctx, id, contentType, confirmRemoveBody, "")
}

func (s *knowledgeService) correctContentType(ctx context.Context, id, contentType string, confirmRemoveBody bool, reviewAction string) (*types.Knowledge, error) {
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
	tenantID := types.MustTenantIDFromContext(ctx)
	before, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if before.SourceKind != "article" || before.ParseStatus != types.ParseStatusCompleted {
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
	if before.SourceUnverified {
		mode = types.ProcessingModeFull
	}
	if before.ProcessingMode == types.ProcessingModeFull && mode == types.ProcessingModeSummaryOnly && !confirmRemoveBody {
		return nil, werrors.NewBadRequestError("confirm_remove_body is required because the article body, body vectors and attachments will be removed")
	}
	if reviewAction == "" && before.ContentType == contentType && before.ContentTypeCorrected && before.ProcessingReviewStatus != types.ProcessingReviewPending {
		return s.GetKnowledgeByID(ctx, id)
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, before.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	overrides, err := before.ProcessOverrides()
	if err != nil {
		return nil, err
	}
	eff := ResolveProcessConfig(kb, overrides)
	fetched := *before
	corrected := reviewAction != "confirm"
	fetched.ContentType, fetched.ContentTypeCorrected, fetched.ProcessingMode = contentType, corrected, mode
	var body string
	var images []docparser.StoredImage
	var result *articleProcessingResult
	var temporaryRefs []string
	committed := false
	defer func() {
		if !committed {
			s.deleteCorrectionFiles(context.WithoutCancel(ctx), kb, temporaryRefs)
		}
	}()
	if before.ProcessingMode == types.ProcessingModeSummaryOnly && mode == types.ProcessingModeFull {
		body, images, temporaryRefs, err = s.fetchCorrectedArticle(ctx, kb, &fetched, eff)
		if err != nil {
			return before, werrors.NewBadRequestError("could not save the full article: " + err.Error())
		}
		result, err = s.callArticleProcessing(ctx, kb, &fetched, body, images)
		if err != nil {
			return before, werrors.NewBadRequestError("could not process the full article: " + err.Error())
		}
	}
	var updated types.Knowledge
	var fileChange *vaultContentTypeChange
	var discardedRefs []string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", id, tenantID).First(&updated).Error; err != nil {
			return err
		}
		if updated.ContentType != before.ContentType || updated.ProcessingMode != before.ProcessingMode || updated.ParseStatus != types.ParseStatusCompleted || updated.ProcessingReviewStatus != before.ProcessingReviewStatus || updated.SourceUnverified != before.SourceUnverified {
			return werrors.NewConflictError("the article changed during conversion; reload it and try again")
		}
		updated.ContentType, updated.ContentTypeCorrected, updated.ProcessingMode, updated.UpdatedAt = contentType, corrected, mode, time.Now()
		if reviewAction == "confirm" {
			updated.ProcessingReviewStatus = types.ProcessingReviewConfirmed
		} else if before.ProcessingReviewStatus == types.ProcessingReviewPending || reviewAction == "correct" {
			updated.ProcessingReviewStatus = types.ProcessingReviewCorrected
		}
		if result != nil {
			updated.FilePath, updated.FileName, updated.FileType = fetched.FilePath, fetched.FileName, fetched.FileType
		}
		if err := tx.Save(&updated).Error; err != nil {
			return err
		}
		transactional := s.contentTypeTransactionService(tx)
		transactionCtx := ctx
		if tenant, ok := ctx.Value(types.TenantInfoContextKey).(*types.Tenant); ok && tenant != nil {
			copy := *tenant
			transactionCtx = context.WithValue(ctx, types.TenantInfoContextKey, &copy)
		}
		if result != nil {
			if err := transactional.persistArticleProcessingResult(transactionCtx, kb, &updated, body, images, eff, result, false); err != nil {
				return err
			}
			if updated.ParseStatus != types.ParseStatusCompleted {
				return errors.New("article conversion did not finish")
			}
		} else if before.ProcessingMode == types.ProcessingModeFull && mode == types.ProcessingModeSummaryOnly {
			discardedRefs, err = transactional.clearCorrectedArticleBody(transactionCtx, &updated)
			if err != nil {
				return err
			}
		}
		if s.vault != nil && before.ProcessingMode != mode {
			read := func(ctx context.Context, ref string) (io.ReadCloser, error) {
				return s.resolveFileServiceForPath(ctx, kb, ref).GetFile(ctx, ref)
			}
			fileChange, err = s.vault.changeCorrectedArticleBody(ctx, tx, &updated, body, read)
			if err != nil {
				return err
			}
		} else if result != nil {
			if err := transactional.storeProcessedBody(ctx, kb, &updated, body, images); err != nil {
				return err
			}
			temporaryRefs = append(temporaryRefs, updated.GetMetadata()["processed_body_path"])
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
		return before, werrors.NewBadRequestError("could not correct the article content type: " + err.Error())
	}
	committed = true
	if result != nil {
		s.bindContentResources(ctx, updated.TenantID, updated.ID, body+" "+updated.FilePath)
	}
	if fileChange != nil {
		if err := fileChange.finish(); err != nil {
			s.vault.recordArticleError(ctx, id, err)
		}
	}
	if s.vault != nil {
		if result != nil {
			// Copies in the original storage are removed only after both the
			// database and the first vault body have been written successfully.
			if err := s.removeParsedBodyCopies(ctx, kb, &updated, body, images); err != nil {
				s.vault.recordArticleError(ctx, id, err)
			}
		}
		s.vault.ScheduleRefresh(ctx, id)
	}
	s.deleteCorrectionFiles(ctx, kb, discardedRefs, id)
	return s.GetKnowledgeByID(ctx, id)
}

func (s *knowledgeService) contentTypeTransactionService(tx *gorm.DB) *knowledgeService {
	copy := *s
	copy.db = tx
	copy.repo = repository.NewKnowledgeRepository(tx)
	copy.chunkRepo = repository.NewChunkRepository(tx)
	copy.tenantRepo = repository.NewTenantRepository(tx)
	copy.retrieveEngine = retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))
	copy.chunkService = NewChunkService(copy.chunkRepo, copy.repo, repository.NewKnowledgeBaseRepository(tx), copy.modelService, copy.retrieveEngine)
	copy.vault, copy.resourceCatalog = nil, nil
	return &copy
}

// fetchCorrectedArticle reuses the reprocessing reader and image resolver but
// does not write processing status or source metadata before conversion succeeds.
func (s *knowledgeService) fetchCorrectedArticle(ctx context.Context, kb *types.KnowledgeBase, k *types.Knowledge, eff types.EffectiveProcessConfig) (body string, images []docparser.StoredImage, refs []string, err error) {
	source, err := articleSourceInfo(k)
	if err != nil {
		return "", nil, nil, err
	}
	url := source.OriginalURL
	if url == "" && (k.Type == "url" || k.Type == "file_url") {
		url = k.Source
	}
	if url == "" {
		return "", nil, nil, errors.New("original URL is missing")
	}
	if err := secutils.ValidateURLForSSRF(url); err != nil {
		return "", nil, nil, err
	}
	var parserOverrides map[string]string
	if overrides, e := k.ProcessOverrides(); e != nil {
		return "", nil, nil, e
	} else if overrides != nil {
		parserOverrides = overrides.ParserEngineOverrides
	}
	parserOverrides = MergeParserEngineOverrides(s.getParserEngineOverridesFromContext(ctx), parserOverrides)
	if err := validateParserEngineOverrideURLs(parserOverrides); err != nil {
		return "", nil, nil, err
	}
	isFile := k.Type == "file_url" || isFileURL(url, "", "")
	fileType := k.FileType
	req := &types.ReadRequest{URL: url, Title: k.Title, ParserEngineOverrides: parserOverrides}
	if isFile {
		name, kind := k.FileName, k.FileType
		data, e := downloadFileFromURL(ctx, url, &name, &kind)
		if e != nil {
			return "", nil, nil, e
		}
		if err := validateImportFileType(kind); err != nil {
			return "", nil, nil, err
		}
		path, e := s.resolveFileService(ctx, kb).SaveBytes(ctx, data, k.TenantID, name, true)
		if e != nil {
			return "", nil, nil, e
		}
		refs = append(refs, path)
		k.FilePath, k.FileName, k.FileType, fileType = path, name, kind, kind
		req.URL, req.FileName, req.FileType, req.FileContent = "", name, kind, data
	}
	engine := eff.ChunkingConfig.ResolveParserEngine("url")
	if isFile {
		engine = eff.ChunkingConfig.ResolveParserEngine(fileType)
	}
	req.ParserEngine = engine
	reader, err := docparser.NewReader(ctx, engine, fileType, !isFile, docparser.ReaderDeps{Overrides: parserOverrides})
	if err != nil {
		return "", nil, refs, err
	}
	parsed, err := s.callDocReaderWithTimeout(ctx, reader, req)
	if err != nil {
		return "", nil, refs, fmt.Errorf("fetch original article: %w", err)
	}
	if parsed == nil {
		return "", nil, refs, errors.New("original reader returned no content")
	}
	sanitizeReadResult(parsed)
	if parsed.Error != "" {
		return "", nil, refs, errors.New(parsed.Error)
	}
	if strings.TrimSpace(parsed.MarkdownContent) == "" {
		return "", nil, refs, errors.New("original article body is empty")
	}
	if s.imageResolver != nil {
		fileSvc := s.resolveFileService(ctx, kb)
		markdown, stored, e := s.imageResolver.ResolveAndStore(ctx, parsed, fileSvc, k.TenantID)
		images = append(images, stored...)
		for _, image := range stored {
			refs = append(refs, image.ServingURL)
		}
		if e != nil {
			return "", images, refs, e
		}
		if markdown != "" {
			parsed.MarkdownContent = markdown
		}
		markdown, stored, e = s.imageResolver.ResolveRemoteImages(ctx, parsed.MarkdownContent, fileSvc, k.TenantID)
		images = append(images, stored...)
		for _, image := range stored {
			refs = append(refs, image.ServingURL)
		}
		if e != nil {
			return "", images, refs, e
		}
		if markdown != "" {
			parsed.MarkdownContent = markdown
		}
	}
	if docparser.HasUnstoredImages(parsed.MarkdownContent) {
		return "", images, refs, errors.New("original article has unstored images")
	}
	return chunker.NormalizeLineEndings(parsed.MarkdownContent), images, refs, nil
}

func (s *knowledgeService) clearCorrectedArticleBody(ctx context.Context, k *types.Knowledge) ([]string, error) {
	if err := s.db.WithContext(ctx).Table("embeddings").Where("knowledge_base_id=? AND knowledge_id=?", k.KnowledgeBaseID, k.ID).Delete(nil).Error; err != nil {
		return nil, err
	}
	if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, k.ID); err != nil {
		return nil, err
	}
	if k.StorageSize > 0 {
		if err := s.tenantRepo.AdjustStorageUsed(ctx, k.TenantID, -k.StorageSize); err != nil {
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
	return refs, s.repo.UpdateKnowledge(ctx, k)
}

func (s *knowledgeService) deleteCorrectionFiles(ctx context.Context, kb *types.KnowledgeBase, refs []string, ownerIDs ...string) {
	seen := map[string]bool{}
	releaser := knowledgeResourceOwners(s.resourceCatalog, ownerIDs...)
	for _, ref := range refs {
		if ref == "" || seen[ref] || strings.HasPrefix(ref, "http:") || strings.HasPrefix(ref, "https:") || strings.HasPrefix(ref, "data:") || !strings.Contains(ref, "://") {
			continue
		}
		seen[ref] = true
		if !releaser.deletable(ctx, ref) {
			continue
		}
		if err := s.resolveFileServiceForPath(ctx, kb, ref).DeleteFile(ctx, ref); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warnf(ctx, "Remove content type conversion copy %s: %v", ref, err)
		}
	}
}
