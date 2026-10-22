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
	}
	if rules.Identity.URL && entry.NormalizedURL != "" {
		keys = append(keys, discardKey{identityFingerprint("url", entry.NormalizedURL), entry.NormalizedURL})
	}
	if hash := strings.TrimSpace(entry.ContentHash); hash != "" {
		keys = append(keys, discardKey{fingerprint: identityFingerprint("body", hash)})
	}
	if rules.Identity.Identifier && entry.Identifier != "" {
		keys = append(keys, discardKey{fingerprint: identityFingerprint("identifier", entry.Identifier)})
	}
	if entry.NormalizedURL == "" {
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
// canonical article in all four state checks.
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
	query := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND related_entry_id=''", spaceID, library.ID).
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
			item.Status, item.EntryID = "in_library", article.ID
			intake, _ := article.IntakeMetadata()
			if intake != nil && intake.Decision != "approved" {
				item.Status = "pending_review"
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// Record every canonical and repost link before destructive deletion. The
// ledger has no FK because it intentionally survives article removal.
func (s *entryService) rememberIngestDiscard(ctx context.Context, k *types.Entry, reason string) error {
	if s.db == nil || k == nil || k.RelatedEntryID != "" {
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
		if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "library_id"}, {Name: "fingerprint"}}, DoUpdates: clause.AssignmentColumns([]string{"entry_id", "discarded_at", "reason"})}).Create(&row).Error; err != nil {
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
