package service

import (
	"context"
	"encoding/json"
	"errors"
	"reliquary/internal/application/repository"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"strings"
	"time"

	"gorm.io/gorm"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func normalizeIntakePublicationDate(raw string) string {
	parsed := parsePublicationTime(raw)
	if parsed == nil {
		return ""
	}
	raw = strings.TrimSpace(raw)
	if _, err := time.Parse(time.DateOnly, raw); err == nil {
		return raw
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

// approveRetainedIntake applies the current admission policy to stored legacy material.
func (s *entryService) approveRetainedIntake(ctx context.Context, entry *types.Entry) error {
	intake, err := entry.IntakeMetadata()
	if err != nil || intake == nil || intake.Decision == "approved" {
		return err
	}
	intake.Decision, intake.Reason = "approved", ""
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return err
	}
	return s.saveIntakeReview(ctx, entry)
}

func (s *entryService) saveIntakeReview(ctx context.Context, entry *types.Entry) error {
	if ctx.Value(libraryReviewContextKey{}) == true {
		// A library review commits its verdict and graph visibility together.
		return nil
	}
	return s.repo.UpdateEntry(ctx, entry)
}

// ReviewEntryIntake reviews pending material or deletes it on user rejection.
// It deliberately does not alter previously approved records.
func (s *entryService) ReviewEntryIntake(ctx context.Context, id, action string, source types.SourceInfo) (reviewed *types.Entry, reviewErr error) {
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			return copy.ReviewEntryIntake(ctx, id, action, source)
		})
	}

	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	library, err := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	if err != nil {
		return nil, err
	}
	entry.LibraryName = library.Name
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewForbiddenError("an authenticated user is required to review intake")
	}
	intake, err := entry.IntakeMetadata()
	retryFailed := action == "retry" && intake != nil && intake.Decision == "pending" && entry.ParseStatus == types.ParseStatusFailed
	if err != nil || intake == nil || (intake.Decision != "rejected" && !retryFailed) {
		return nil, werrors.NewBadRequestError("entry is not awaiting intake review")
	}
	before := *intake
	before.SourceInfo = intake.SourceInfo
	missingLink := strings.TrimSpace(intake.SourceInfo.OriginalURL) == ""
	if action == "admit_unverified" {
		if !missingLink || !source.Confirmed || strings.TrimSpace(source.OriginalURL) != "" {
			return nil, werrors.NewBadRequestError("collecting without an original URL requires explicit confirmation")
		}
		entry.SourceUnverified = true
	}
	if action == "supplement_url" && (!missingLink || len(strings.TrimSpace(source.OriginalURL)) > 2048 || !validIngestURL(strings.TrimSpace(source.OriginalURL))) {
		return nil, werrors.NewBadRequestError("supplement_url requires a valid HTTP or HTTPS original URL")
	}
	defer func() {
		if reviewErr != nil || reviewed == nil {
			return
		}
		if s.vault != nil {
			s.vault.ScheduleRefresh(ctx, id)
		}
	}()
	if action == "reject" && !source.Confirmed {
		return nil, werrors.NewBadRequestError("rejecting intake deletes its stored source and requires confirmation")
	}
	if action == "reject" || action == "discard" {
		if err := s.removeEntryDiagnosticMaterials(ctx, entry); err != nil {
			return nil, err
		}
		discard := func(svc *entryService) error {
			if svc.db != nil {
				if err := svc.rememberIngestDiscard(ctx, entry, ingestDiscardReason(ctx)); err != nil {
					return err
				}
				if err := NewOperationLogService(svc.db).RecordTx(ctx, svc.db, "review_"+action, id, before, map[string]any{"reason": ingestDiscardReason(ctx)}); err != nil {
					return err
				}
				if err := svc.cleanupLibraryRebuild(ctx, entry); err != nil {
					return err
				}
				if err := svc.db.Unscoped().Where("entry_id=? AND space_id=?", id, spaceID).Delete(&types.Chunk{}).Error; err != nil {
					return err
				}
			}
			if err := svc.clearIntakeSource(ctx, entry); err != nil {
				return err
			}
			if err := svc.releaseIngestArticleAssets(ctx, id); err != nil {
				return err
			}
			if err := deleteEntryAssociations(ctx, svc.db, spaceID, []string{id}); err != nil {
				return err
			}
			if err := svc.repo.HardDeleteEntry(ctx, spaceID, id); err != nil {
				return err
			}
			if svc.vault != nil {
				return svc.vault.DeleteArticle(ctx, entry)
			}
			return nil
		}
		if s.db == nil {
			return entry, discard(s)
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			copy := *s
			copy.db = tx
			copy.repo = repository.NewEntryRepository(tx)
			return discard(&copy)
		})
		return entry, err
	}
	if action != "retry" && action != "approve" && action != "supplement_url" && action != "admit_unverified" {
		return nil, werrors.NewBadRequestError("invalid intake review action")
	}
	if action == "retry" {
		if err := advanceSegmentGeneration(entry, "review"); err != nil {
			return nil, err
		}
	}
	if source.Author != "" {
		intake.SourceInfo.Author = source.Author
	}
	if source.Publisher != "" {
		intake.SourceInfo.Publisher = source.Publisher
	}
	if source.OriginalURL != "" {
		entry.SourceUnverified = false
		intake.SourceInfo.OriginalURL = strings.TrimSpace(source.OriginalURL)
		intake.SourceInfo.OriginalURLSource = "user_supplemented"
	}
	if source.PublishedAt != "" {
		intake.SourceInfo.PublishedAt = source.PublishedAt
	}
	intake.SourceInfo.PublishedAt = normalizeIntakePublicationDate(intake.SourceInfo.PublishedAt)
	approved := action != "retry"
	intake.Decision, intake.Reason, intake.UserRejected = "pending", "", false
	if approved {
		intake.Decision = "approved"
	}
	entry.ParseStatus, entry.ErrorMessage, entry.UpdatedAt = types.ParseStatusPending, "", time.Now()
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
		return nil, err
	}
	if _, forced := metadata["intake_reject_reason"]; forced {
		delete(metadata, "intake_reject_reason")
		value, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		entry.Metadata = types.JSON(value)
	}
	if err := s.saveIntakeUserDecision(ctx, entry, action, before); err != nil {
		return nil, err
	}
	defer func() {
		if reviewErr != nil && !types.IsChatGPTDeferred(reviewErr) {
			s.failIntake(ctx, entry)
		}
	}()
	body, available, err := s.reprocessStoredBody(ctx, entry)
	if err != nil {
		return nil, err
	}
	if available {
		if action == "retry" || entry.VaultPath != "" {
			return entry, s.enqueueRetainedIntakeReview(ctx, entry, body, approved)
		}
		_, err = s.enqueueManualProcessing(ctx, entry, body, false)
		return entry, err
	}
	return nil, werrors.NewBadRequestError("original is not available")
}

func (s *entryService) saveIntakeUserDecision(ctx context.Context, entry *types.Entry, action string, before any) error {
	if s.db == nil {
		return s.repo.UpdateEntry(ctx, entry)
	}
	after, err := entry.IntakeMetadata()
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "review_"+action, entry.ID, before, after); err != nil {
			return err
		}
		if action == "admit_unverified" || action == "supplement_url" {
			if err := tx.Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).
				Updates(map[string]any{"source_unverified": action == "admit_unverified"}).Error; err != nil {
				return err
			}
		}
		return repository.NewEntryRepository(tx).UpdateEntry(ctx, entry)
	})
}

// recordIntakeImages keeps partial parse output reachable for cleanup even when
// the next download fails, before there are any indexed chunks.
func (s *entryService) recordIntakeImages(ctx context.Context, k *types.Entry, images []docparser.StoredImage) error {
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || len(images) == 0 {
		return err
	}
	seen := make(map[string]bool)
	for _, ref := range intake.Images {
		seen[ref] = true
	}
	for _, img := range images {
		if img.ServingURL != "" && !seen[img.ServingURL] {
			intake.Images = append(intake.Images, img.ServingURL)
			seen[img.ServingURL] = true
		}
	}
	s.bindContentResources(ctx, k.SpaceID, k.ID, strings.Join(intake.Images, " "))
	if err := k.SetIntakeMetadata(intake); err != nil {
		return err
	}
	return s.repo.UpdateEntry(ctx, k)
}

func (s *entryService) saveIntakeOriginal(ctx context.Context, library *types.Library, k *types.Entry, body string, metadata map[string]string) error {
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || intake.Decision != "pending" {
		return err
	}
	if intake.SourceInfo.Author == "" {
		intake.SourceInfo.Author = metadata["author"]
	}
	if intake.SourceInfo.Publisher == "" {
		intake.SourceInfo.Publisher = metadata["publisher"]
	}
	if k.SourceKind != "mine" && intake.SourceInfo.OriginalURL == "" {
		intake.SourceInfo.OriginalURL = metadata["original_url"]
		if intake.SourceInfo.OriginalURL != "" {
			intake.SourceInfo.OriginalURLSource = "from_markdown"
		}
	}
	if intake.SourceInfo.PublishedAt == "" {
		intake.SourceInfo.PublishedAt = metadata["published_at"]
	}
	if err := k.SetIntakeMetadata(intake); err != nil {
		return err
	}
	if err := extractIntakeOriginalLink(k, body, metadata); err != nil {
		return err
	}
	intake, _ = k.IntakeMetadata()
	// Keep legacy file references available for recovery and terminal cleanup.
	k.VaultBody = body
	if err := k.SetIntakeMetadata(intake); err != nil {
		return err
	}
	if err := s.repo.UpdateEntry(ctx, k); err != nil {
		return err
	}
	return nil
}

// clearIntakeSource only handles pre-index messages. Shared resources are
// released through the existing ownership catalog before deleting their bytes.
func (s *entryService) clearIntakeSource(ctx context.Context, k *types.Entry) error {
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || intake.Decision == "approved" {
		return err
	}
	// Upload configuration belongs to the processing attempt, even after
	// its source is removed. Retain the raw value so unknown options survive.
	var sourceMetadata map[string]json.RawMessage
	if err := json.Unmarshal(k.Metadata, &sourceMetadata); err != nil {
		return err
	}
	retainedMetadata := make(map[string]json.RawMessage)
	if overrides, ok := sourceMetadata["process_overrides"]; ok {
		retainedMetadata["process_overrides"] = overrides
	}
	metadata, err := json.Marshal(retainedMetadata)
	if err != nil {
		return err
	}
	releaser := entryResourceOwners(s.resourceCatalog, k.ID)
	refs := append([]string{k.FilePath, intake.OriginalPath}, intake.Images...)
	seen := make(map[string]bool)
	var cleanupErr error
	for _, ref := range refs {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		if !releaser.deletable(ctx, ref) {
			continue
		}
		cleanupErr = errors.Join(cleanupErr, s.resolveFileServiceForPath(ctx, nil, ref).DeleteFile(ctx, ref))
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	intake.OriginalPath, intake.Images = "", nil
	k.FilePath, k.Description = "", ""
	k.Metadata, k.CustomMetadata = types.JSON(metadata), nil
	if err := k.SetIntakeMetadata(intake); err != nil {
		return err
	}
	return s.repo.UpdateEntry(ctx, k)
}

func (s *entryService) failIntake(ctx context.Context, k *types.Entry) {
	if k.ParseStatus == types.ParseStatusCompleted {
		return
	}
	if s.taskEnqueueFailedInTransaction() || ctx.Err() != nil {
		// An interrupted worker will run again from its durable queue record.
		// Keep the stored original available to that recovered execution.
		return
	}
	if s.hasSegmentCheckpoint(ctx, k) {
		if k.ParseStatus != types.ParseStatusFailed {
			reason := strings.TrimSpace(k.ErrorMessage)
			if reason == "" {
				reason = "message processing failed"
			}
			s.markArticleProcessingFailed(ctx, k, errors.New(reason))
		}
		s.tracker().FinalizeAttempt(ctx, k.ID, s.tracker().LatestAttempt(ctx, k.ID), "failed", nil, "intake_processing_failed", k.ErrorMessage)
		return
	}
	intake, err := k.IntakeMetadata()
	if err != nil || intake == nil || intake.Decision != "pending" {
		return
	}
	reason := strings.TrimSpace(k.ErrorMessage)
	if reason == "" {
		reason = "message processing failed"
	}
	cleanupErr := s.clearIntakeSource(ctx, k)
	intake, _ = k.IntakeMetadata()
	s.recordTerminalMutationFailure()
	intake.Decision, intake.Reason = "error", reason
	k.ParseStatus, k.EnableStatus, k.ErrorMessage = types.ParseStatusFailed, "disabled", intake.Reason
	if cleanupErr != nil {
		k.ErrorMessage += "; source cleanup failed"
		logger.Warnf(ctx, "Intake %s source cleanup failed", k.ID)
	}
	_ = k.SetIntakeMetadata(intake)
	_ = s.repo.UpdateEntry(ctx, k)
	s.tracker().FinalizeAttempt(ctx, k.ID, s.tracker().LatestAttempt(ctx, k.ID), "failed", nil, "intake_processing_failed", k.ErrorMessage)
}
