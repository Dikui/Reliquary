package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

func (s *entryService) lookupIngestDiscard(ctx context.Context, normalized string) (*types.IngestDiscard, error) {
	if normalized == "" {
		return nil, nil
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	var item types.IngestDiscard
	err := s.db.WithContext(ctx).Where("space_id=? AND normalized_url=?", spaceID, normalized).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

// Pending intake is deliberately included. Repost origins resolve to their
// canonical article in all four state checks and in later supplements.
func (s *entryService) findIngestURL(ctx context.Context, normalized string, lock bool) (*types.Entry, error) {
	if normalized == "" {
		return nil, nil
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	owners := s.db.Model(&types.EntryRepostSource{}).Select("entry_id").Where("space_id=? AND normalized_url=?", spaceID, normalized)
	query := s.db.WithContext(ctx).Where("space_id=? AND source_kind='article' AND related_article_id=''", spaceID).
		Where("normalized_url=? OR id IN (?)", normalized, owners).
		Where("parse_status NOT IN ?", []string{types.ParseStatusDeleting})
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var item types.Entry
	err := query.Order("created_at ASC,id ASC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (s *entryService) CheckURLs(ctx context.Context, urls []string) ([]types.IngestCheckResult, error) {
	if len(urls) < 1 || len(urls) > types.MaxIngestCheckURLs {
		return nil, werrors.NewBadRequestError("urls must contain between 1 and 100 links")
	}
	if _, _, _, err := s.ingestIdentity(ctx); err != nil {
		return nil, err
	}
	result := make([]types.IngestCheckResult, 0, len(urls))
	for _, raw := range urls {
		if !validIngestURL(strings.TrimSpace(raw)) || len(raw) > 2048 {
			return nil, werrors.NewBadRequestError("urls must be absolute HTTP or HTTPS links of at most 2048 bytes")
		}
		normalized := normalizeMessageURL(raw)
		item := types.IngestCheckResult{URL: raw, Status: "not_found"}
		if discarded, err := s.lookupIngestDiscard(ctx, normalized); err != nil {
			return nil, err
		} else if discarded != nil {
			item.Status, item.EntryID = "discarded", discarded.EntryID
		} else if article, err := s.findIngestURL(ctx, normalized, false); err != nil {
			return nil, err
		} else if article != nil {
			item.Status, item.EntryID, item.Incomplete = "in_library", article.ID, article.Incomplete
			intake, _ := article.IntakeMetadata()
			if intake != nil && intake.Decision != "approved" {
				item.Status = "pending_review"
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *entryService) ListIncomplete(ctx context.Context, page, size int, includeAttempted bool) (*types.IncompleteListResult, error) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if page < 1 || size < 1 || size > 100 {
		return nil, werrors.NewBadRequestError("invalid pagination")
	}
	if _, _, _, err := s.ingestIdentity(ctx); err != nil {
		return nil, err
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	query := s.db.WithContext(ctx).Model(&types.Entry{}).Where("space_id=? AND source_kind='article' AND incomplete=true", spaceID).
		Where("parse_status NOT IN ?", []string{types.ParseStatusRejected, types.ParseStatusDeleting, types.ParseStatusCancelled}).
		Where("COALESCE(metadata::jsonb->'intake'->>'decision','approved')='approved'")
	if !includeAttempted {
		query = query.Where("external_completion_attempted=false")
	}
	result := &types.IncompleteListResult{Items: []types.IncompleteArticle{}, Page: page, PageSize: size}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	var articles []types.Entry
	if err := query.Order("created_at ASC,id ASC").Offset((page - 1) * size).Limit(size).Find(&articles).Error; err != nil {
		return nil, err
	}
	for _, article := range articles {
		url := article.Source
		if intake, _ := article.IntakeMetadata(); intake != nil && intake.SourceInfo.OriginalURL != "" {
			url = intake.SourceInfo.OriginalURL
		}
		result.Items = append(result.Items, types.IncompleteArticle{EntryID: article.ID, URL: url, Title: article.Title, Acquisition: article.Acquisition, Missing: article.IngestMissing(), ExternalCompletionAttempted: article.ExternalCompletionAttempted})
	}
	return result, nil
}

func (s *entryService) AcceptIncomplete(ctx context.Context, id string) (*types.IngestResult, error) {
	ctx, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	var article types.Entry
	var summaryBody string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(&article).Error; err != nil {
			return err
		}
		if !article.Incomplete {
			return werrors.NewBadRequestError("article is not incomplete")
		}
		if intake, _ := article.IntakeMetadata(); intake != nil && intake.Decision != "approved" {
			return werrors.NewBadRequestError("intake must be reviewed before accepting missing material")
		}
		if article.ProcessingMode == types.ProcessingModeSummaryOnly && article.ParseStatus == types.ParseStatusCompleted {
			kb, err := s.kbService.GetKnowledgeBaseByID(ctx, article.KnowledgeBaseID)
			if err != nil {
				return err
			}
			body, err := s.readIngestBody(ctx, kb, &article)
			if err != nil {
				return err
			}
			summaryBody = body
		}
		before := article.IngestMissing()
		article.Incomplete = false
		article.LastIngestAPIKeyID, article.LastIngestAPIKeyName = keyID, keyName
		if err := tx.Model(&article).Updates(map[string]any{"incomplete": false, "last_ingest_api_key_id": keyID, "last_ingest_api_key_name": keyName, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "accept_incomplete", id, map[string]any{"missing": before}, map[string]any{"incomplete": false, "accepted_missing": before})
	})
	if err != nil {
		return nil, err
	}
	if article.ProcessingMode == types.ProcessingModeSummaryOnly && article.ParseStatus == types.ParseStatusCompleted {
		if err := s.cleanupAcceptedSummaryMaterial(ctx, &article, summaryBody); err != nil {
			logger.Errorf(ctx, "Accepted summary article %s material cleanup failed: %v", id, err)
		}
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	result := ingestResult(&article, "accepted", "已接受缺失，文章已定稿。", "", "")
	result.Completeness = []types.IngestMissing{}
	return result, nil
}

// Record every canonical and repost link before destructive deletion. The
// ledger has no FK because it intentionally survives article removal.
func (s *entryService) rememberIngestDiscard(ctx context.Context, k *types.Entry, reason string) error {
	if s.db == nil || k == nil || k.SourceKind != "article" || k.RelatedArticleID != "" {
		return nil
	}
	links := []string{k.NormalizedURL, normalizeMessageURL(k.Source)}
	if intake, _ := k.IntakeMetadata(); intake != nil {
		links = append(links, normalizeMessageURL(intake.SourceInfo.OriginalURL))
	}
	var reposts []types.EntryRepostSource
	if err := s.db.WithContext(ctx).Where("space_id=? AND entry_id=?", k.SpaceID, k.ID).Find(&reposts).Error; err != nil {
		return err
	}
	for _, source := range reposts {
		links = append(links, source.NormalizedURL)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "用户丢弃"
	}
	for _, normalized := range uniqueEntryIDs(links) {
		row := types.IngestDiscard{SpaceID: k.SpaceID, NormalizedURL: normalized, EntryID: k.ID, DiscardedAt: time.Now(), Reason: reason}
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "normalized_url"}}, DoUpdates: clause.AssignmentColumns([]string{"article_id", "discarded_at", "reason"})}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}

type ingestDiscardReasonKey struct{}

func ingestDiscardReason(ctx context.Context) string {
	if reason, ok := ctx.Value(ingestDiscardReasonKey{}).(string); ok && strings.TrimSpace(reason) != "" {
		return reason
	}
	return "用户丢弃"
}
func (s *entryService) DiscardIngest(ctx context.Context, id, reason string) error {
	if s.needsTaskTransaction() {
		_, err := entryTaskTransaction(ctx, s, func(copy *entryService) (bool, error) { return true, copy.DiscardIngest(ctx, id, reason) })
		return err
	}
	ctx, _, _, err := s.ingestIdentity(ctx)
	if err != nil {
		return err
	}
	return s.DeleteEntry(context.WithValue(ctx, ingestDiscardReasonKey{}, reason), id)
}

// Once missing material is accepted, an already processed summary article has
// no further need for the private bytes held while completion was possible.
func (s *entryService) cleanupAcceptedSummaryMaterial(ctx context.Context, k *types.Entry, body string) error {
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, k.KnowledgeBaseID)
	if err != nil {
		return err
	}
	attachments := k.IngestAttachmentRecords()
	if err := s.removeSummaryOnlyBody(ctx, kb, k, body, nil); err != nil {
		return err
	}
	releaser := entryResourceOwners(s.resourceCatalog, k.ID)
	for _, attachment := range attachments {
		if (!strings.HasPrefix(attachment.Path, "local://") && !strings.HasPrefix(attachment.Path, "resource://")) || !releaser.deletable(ctx, attachment.Path) {
			continue
		}
		if err := s.resolveFileServiceForPath(ctx, kb, attachment.Path).DeleteFile(ctx, attachment.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := k.SetIngestAttachments([]types.IngestAttachment{}); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).Update("ingest_attachments", k.IngestAttachments).Error
}
