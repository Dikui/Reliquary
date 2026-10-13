package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"strings"
	"time"

	taskqueue "reliquary/internal/queue"

	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// enqueueManualProcessing enqueues a manual:process PostgreSQL task for async cleanup + re-indexing.
func (s *entryService) enqueueManualProcessing(ctx context.Context,
	entry *types.Entry, content string, needCleanup bool, extraOptions ...taskqueue.Option,
) (string, error) {
	requestID, _ := types.RequestIDFromContext(ctx)
	payload := types.ManualProcessPayload{
		RequestId:   requestID,
		SpaceID:     entry.SpaceID,
		EntryID:     entry.ID,
		LibraryID:   entry.LibraryID,
		Content:     content,
		NeedCleanup: needCleanup,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manual process payload: %w", err)
	}

	options := append([]taskqueue.Option{taskqueue.Queue(types.QueueDefault), taskqueue.MaxRetry(3), taskqueue.Timeout(30 * time.Minute)}, extraOptions...)
	task := taskqueue.NewTask(types.TypeManualProcess, payloadBytes, options...)
	info, err := s.task.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue manual process task: %w", err)
	}
	logger.Infof(ctx, "Enqueued manual process task: entry_id=%s, task_id=%s", entry.ID, info.ID)
	return info.ID, nil
}

// markEntryEnqueueFailed prevents a durable entry row from remaining
// indefinitely "pending" when its background processing task was never
// created. The API may still return the row so callers can retry it.
func (s *entryService) markEntryEnqueueFailed(ctx context.Context, entry *types.Entry) {
	if entry == nil {
		return
	}
	defer s.failIntake(ctx, entry)
	entry.ParseStatus = "failed"
	entry.ErrorMessage = "Failed to enqueue processing task"
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		logger.Errorf(ctx, "Failed to mark entry as failed after enqueue error: %v", err)
	}
}

// sanitizeManualDownloadFilename converts a entry title into a safe .md
// download filename. Characters that are illegal or dangerous in HTTP header
// values and file-system paths are removed or replaced; a blank result falls
// back to "untitled".
func sanitizeManualDownloadFilename(title string) string {
	safeName := strings.NewReplacer(
		"\n", "", "\r", "", "\t", "", "/", "-", "\\", "-", "\"", "'",
	).Replace(title)
	if strings.TrimSpace(safeName) == "" {
		safeName = "untitled"
	}
	if !strings.HasSuffix(strings.ToLower(safeName), manualFileExtension) {
		safeName += manualFileExtension
	}
	return safeName
}

// bindContentResources claims every stored file the body references on behalf
// of a entry entry.
//
// A manual document may refer to previously stored resources. Claims keep
// those files available while the document uses them. References from another
// workspace are skipped.
//
// Best-effort by design: the document is already saved, and a missed claim
// degrades to the old behaviour rather than failing the save.
func (s *entryService) bindContentResources(
	ctx context.Context, spaceID uint64, entryID, content string,
) {
	if s.resourceCatalog == nil || entryID == "" {
		return
	}
	for _, ref := range types.ScanResourceReferences(content) {
		resource, err := s.resourceCatalog.Resolve(ctx, ref)
		if err != nil || resource == nil {
			logger.Warnf(ctx, "Skip binding unknown resource %s to entry %s: %v", ref, entryID, err)
			continue
		}
		if resource.SpaceID != spaceID {
			logger.Warnf(ctx, "Skip binding cross-workspace resource %s to entry %s", ref, entryID)
			continue
		}
		if err := s.resourceCatalog.Bind(
			ctx, ref, types.ResourceOwnerEntry, entryID, types.ResourceRelationAttachment,
		); err != nil {
			logger.Warnf(ctx, "Failed to bind resource %s to entry %s: %v", ref, entryID, err)
		}
	}
}

func (s *entryService) triggerManualProcessing(ctx context.Context,
	library *types.Library, entry *types.Entry, content string, _ bool,
) error {
	defer func() {
		if entry.ParseStatus == types.ParseStatusFailed {
			s.failIntake(ctx, entry)
		}
	}()
	clean := strings.TrimSpace(content)
	if clean == "" {
		return errors.New("article body is empty")
	}
	clean = content
	if err := s.recordMissingIngestImages(ctx, entry, clean); err != nil {
		return err
	}

	// Store embedded image bytes before chunking.
	// Runs before chunking so chunks contain stable provider:// URLs.
	var resolvedImages []docparser.StoredImage
	if s.imageResolver != nil && !(s.vault != nil && entry.VaultPath != "" && entry.ProcessingMode == types.ProcessingModeFull) {
		fileSvc := s.resolveFileService(ctx, library)
		afterDataURI, fromDataURI, dataErr := s.imageResolver.ResolveAndStore(ctx, &types.ReadResult{MarkdownContent: clean}, fileSvc, entry.SpaceID)
		if err := s.recordIntakeImages(ctx, entry, fromDataURI); err != nil {
			entry.ParseStatus = types.ParseStatusFailed
			return errors.New("article processing failed")
		}
		if dataErr != nil {
			if intake, _ := entry.IntakeMetadata(); intake != nil {
				entry.ParseStatus, entry.ErrorMessage, entry.UpdatedAt = types.ParseStatusFailed, "failed to resolve intake images", time.Now()
				_ = s.repo.UpdateEntry(ctx, entry)
				return errors.New("article processing failed")
			}
		}
		if len(fromDataURI) > 0 {
			logger.Infof(ctx, "Resolved %d data-URI images for manual entry %s", len(fromDataURI), entry.ID)
			clean = afterDataURI
			resolvedImages = append(resolvedImages, fromDataURI...)
		}
		if err := s.recordMissingIngestImages(ctx, entry, clean); err != nil {
			return err
		}
		if intake, _ := entry.IntakeMetadata(); intake != nil && s.hasUnstoredArticleImages(entry, clean) {
			entry.ParseStatus, entry.ErrorMessage, entry.UpdatedAt = types.ParseStatusFailed, "intake has unstored content images", time.Now()
			_ = s.repo.UpdateEntry(ctx, entry)
			return errors.New("article processing failed")
		}
	}
	if err := extractIntakeOriginalLink(entry, clean, nil, entry.FileType, nil); err != nil {
		entry.ParseStatus = types.ParseStatusFailed
		return err
	}
	if intake, _ := entry.IntakeMetadata(); intake != nil && intake.Decision != "approved" {
		existing, err := s.checkMessageDuplicate(ctx, entry, clean)
		if err != nil {
			entry.ParseStatus = types.ParseStatusFailed
			return errors.New("article processing failed")
		}
		if existing != nil {
			if err := s.stopDuplicateProcessing(ctx, entry); err != nil {
				entry.ParseStatus = types.ParseStatusFailed
				return err
			}
			return nil
		}
	}

	if intake, _ := entry.IntakeMetadata(); intake != nil {
		if s.hasUnstoredArticleImages(entry, clean) {
			entry.ParseStatus = types.ParseStatusFailed
			return errors.New("article processing failed")
		}
		if err := s.saveIntakeOriginal(ctx, library, entry, clean, nil); err != nil {
			entry.ParseStatus = types.ParseStatusFailed
			return errors.New("article processing failed")
		}
	}

	// Re-claim the body's stored files. This runs after cleanupEntryResources
	// released the previous run's claims, so a republished document keeps the
	// files its new body still references.
	s.bindContentResources(ctx, entry.SpaceID, entry.ID, clean)

	// Keep manually entered CRLF text aligned with the LF values sent by the
	// chunking preview endpoint.
	clean = chunker.NormalizeLineEndings(clean)

	processOverrides, _ := entry.ProcessOverrides()
	eff := ResolveProcessConfig(library, processOverrides)

	if intake, _ := entry.IntakeMetadata(); intake != nil {
		rejected, err := s.reviewIntake(ctx, library, entry, clean)
		if err != nil {
			s.markArticleProcessingFailed(ctx, entry, err)
			return err
		}
		if rejected {
			return nil
		}
	}
	if err := s.processArticleBody(ctx, library, entry, clean, resolvedImages, eff); err != nil {
		s.markArticleProcessingFailed(ctx, entry, err)
		return err
	}
	return nil
}
