package service

import (
	"context"
	"errors"
	"strings"
	"time"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type discardKey struct{ fingerprint, url string }

func entryDiscardKeys(entry *types.Entry, rules types.TemplateRules) []discardKey {
	keys := []discardKey{}
	if rules.Identity.SourceRef && entry.SourceRef != "" {
		hash := entry.SourceBodyHash
		if hash == "" {
			hash = strings.TrimSpace(entry.ContentHash)
		}
		if hash != "" {
			keys = append(keys, discardKey{fingerprint: identityFingerprint("source_ref", entry.SourceRef+"\x00"+hash)})
		}
		return keys
	}
	if rules.Identity.URL && entry.NormalizedURL != "" {
		keys = append(keys, discardKey{identityFingerprint("url", entry.NormalizedURL), entry.NormalizedURL})
	}
	if hash := strings.TrimSpace(entry.ContentHash); rules.Identity.Body && hash != "" {
		keys = append(keys, discardKey{fingerprint: identityFingerprint("body", hash)})
	}
	if rules.Identity.Identifier && entry.Identifier != "" {
		keys = append(keys, discardKey{fingerprint: identityFingerprint("identifier", entry.Identifier)})
	}
	if rules.Identity.PublisherDate && entry.NormalizedURL == "" {
		if key := timelySourceIdentity(entry); key != "" {
			keys = append(keys, discardKey{fingerprint: key})
		}
	}
	return keys
}

func (s *entryService) lookupIngestDiscard(ctx context.Context, libraryID string, keys []discardKey) (*types.IngestDiscard, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	fingerprints := []string{}
	for _, key := range keys {
		fingerprints = append(fingerprints, key.fingerprint)
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	var item types.IngestDiscard
	err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND fingerprint IN ?", spaceID, libraryID, fingerprints).Order("discarded_at DESC").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

// Pending intake is deliberately included. Repost origins resolve to their
// canonical article in all four state checks and in later supplements.
func (s *entryService) findIngestURL(ctx context.Context, normalized string, lock bool, library *types.Library) (*types.Entry, error) {
	if normalized == "" {
		return nil, nil
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	rules := intakeTemplateRules(library)
	if !rules.Identity.URL {
		return nil, nil
	}
	owners := s.db.Model(&types.EntryRepostSource{}).Select("entry_id").Where("space_id=? AND library_id=? AND normalized_url=?", spaceID, library.ID, normalized)
	query := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND related_article_id=''", spaceID, library.ID).
		Where("parse_status NOT IN ?", []string{types.ParseStatusDeleting})
	if rules.MergeReposts {
		query = query.Where("normalized_url=? OR id IN (?)", normalized, owners)
	} else {
		query = query.Where("normalized_url=?", normalized)
	}
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

func (s *entryService) CheckURLs(ctx context.Context, urls []string, selectors ...string) ([]types.IngestCheckResult, error) {
	if len(urls) < 1 || len(urls) > types.MaxIngestCheckURLs {
		return nil, werrors.NewBadRequestError("urls must contain between 1 and 100 links")
	}
	if _, _, _, err := s.ingestIdentity(ctx); err != nil {
		return nil, err
	}
	selector := ""
	if len(selectors) > 0 {
		selector = selectors[0]
	}
	library, libraryErr := s.resolveIngestLibrary(ctx, selector)
	result := make([]types.IngestCheckResult, 0, len(urls))
	for _, raw := range urls {
		if libraryErr != nil {
			result = append(result, types.IngestCheckResult{URL: raw, Status: "invalid", Reason: libraryErr.Error()})
			continue
		}
		if !validIngestURL(strings.TrimSpace(raw)) || len(raw) > 2048 {
			return nil, werrors.NewBadRequestError("urls must be absolute HTTP or HTTPS links of at most 2048 bytes")
		}
		normalized := normalizeMessageURL(raw)
		item := types.IngestCheckResult{URL: raw, Status: "not_found"}
		if discarded, err := s.lookupIngestDiscard(ctx, library.ID, entryDiscardKeys(&types.Entry{NormalizedURL: normalized}, intakeTemplateRules(library))); err != nil {
			return nil, err
		} else if discarded != nil {
			item.Status, item.EntryID = "discarded", discarded.EntryID
		} else if article, err := s.findIngestURL(ctx, normalized, false, library); err != nil {
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
	query := s.db.WithContext(ctx).Model(&types.Entry{}).Where("space_id=? AND source_kind IN ('article','mine') AND incomplete=true", spaceID).
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
	templates := map[string]string{}
	names := map[string]string{}
	var libraries []types.Library
	ids := []string{}
	for _, article := range articles {
		ids = append(ids, article.LibraryID)
	}
	if len(ids) > 0 {
		if err := s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, ids).Find(&libraries).Error; err != nil {
			return nil, err
		}
	}
	for _, library := range libraries {
		templates[library.ID] = library.Template
		names[library.ID] = library.Name
	}
	for _, article := range articles {
		url := article.Source
		if intake, _ := article.IntakeMetadata(); intake != nil && intake.SourceInfo.OriginalURL != "" {
			url = intake.SourceInfo.OriginalURL
		}
		result.Items = append(result.Items, types.IncompleteArticle{LibraryName: names[article.LibraryID], LibraryTemplate: templates[article.LibraryID], LibraryID: article.LibraryID, EntryID: article.ID, URL: url, Title: article.Title, Acquisition: article.Acquisition, SourceUnverified: article.SourceUnverified, Missing: article.IngestMissing(), ExternalCompletionAttempted: article.ExternalCompletionAttempted})
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

	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	if err := populateEntryLibraryNames(ctx, s.db, []*types.Entry{&article}); err != nil {
		return nil, err
	}
	result := ingestResult(&article, "accepted", "已接受缺失，文章已定稿。", "", "")
	result.Completeness = []types.IngestMissing{}
	return result, nil
}

// Record every canonical and repost link before destructive deletion. The
// ledger has no FK because it intentionally survives article removal.
func (s *entryService) rememberIngestDiscard(ctx context.Context, k *types.Entry, reason string) error {
	if s.db == nil || k == nil || k.RelatedArticleID != "" {
		return nil
	}
	rules, err := messageTemplateRules(s.db.WithContext(ctx), k)
	if err != nil {
		return err
	}
	setMessageFingerprint(k, "")
	keys := entryDiscardKeys(k, rules)
	if rules.MergeReposts {
		var reposts []types.EntryRepostSource
		if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND entry_id=?", k.SpaceID, k.LibraryID, k.ID).Find(&reposts).Error; err != nil {
			return err
		}
		for _, source := range reposts {
			if source.NormalizedURL != "" {
				keys = append(keys, discardKey{identityFingerprint("url", source.NormalizedURL), source.NormalizedURL})
			}
		}
	}
	if strings.TrimSpace(reason) == "" {
		reason = "用户丢弃"
	}
	for _, key := range keys {
		row := types.IngestDiscard{SpaceID: k.SpaceID, LibraryID: k.LibraryID, Fingerprint: key.fingerprint, NormalizedURL: key.url, EntryID: k.ID, DiscardedAt: time.Now(), Reason: reason}
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "library_id"}, {Name: "fingerprint"}}, DoUpdates: clause.AssignmentColumns([]string{"article_id", "discarded_at", "reason"})}).Create(&row).Error; err != nil {
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
