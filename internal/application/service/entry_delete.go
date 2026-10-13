package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"

	"reliquary/internal/application/repository"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// collectImageURLs extracts unique provider:// image URLs from image_info JSON strings.
func collectImageURLs(ctx context.Context, imageInfos []string) []string {
	seen := make(map[string]struct{})
	var urls []string
	for _, info := range imageInfos {
		if info == "" {
			continue
		}
		var images []*types.ImageInfo
		if err := json.Unmarshal([]byte(info), &images); err != nil {
			logger.Warnf(ctx, "Failed to parse image_info JSON: %v", err)
			continue
		}
		for _, img := range images {
			if img.URL != "" {
				if _, exists := seen[img.URL]; !exists {
					seen[img.URL] = struct{}{}
					urls = append(urls, img.URL)
				}
			}
		}
	}
	return urls
}

// entryResourceOwners builds the releaser for a set of entry entries
// being deleted. A nil catalog (no resource registry) yields nil, which
// deleteExtractedImages treats as "delete unconditionally", i.e. the behaviour
// from before bindings were tracked.
func entryResourceOwners(catalog interfaces.ResourceCatalog, entryIDs ...string) *resourceReleaser {
	if catalog == nil || len(entryIDs) == 0 {
		return nil
	}
	return &resourceReleaser{catalog: catalog, ownerType: types.ResourceOwnerEntry, ownerIDs: entryIDs}
}

// resourceReleaser decides whether a stored file's bytes may be deleted along
// with the domain object that referenced them.
//
// A file can be claimed by more than one owner. Two entry entries can
// share an image. Deleting one owner must drop only that
// owner's claim; the bytes go away when the last claim does.
type resourceReleaser struct {
	catalog   interfaces.ResourceCatalog
	ownerType string
	ownerIDs  []string
}

// deletable drops this releaser's claims on ref and reports whether the bytes
// are now unreferenced.
//
// An unreadable binding count keeps the file. The cost of keeping it is an
// orphaned blob that a later delete can still reclaim; the cost of guessing
// wrong the other way is an image vanishing from another article
// that nobody deleted.
func (r *resourceReleaser) deletable(ctx context.Context, ref string) bool {
	if r == nil || r.catalog == nil {
		return true
	}
	remaining := int64(-1)
	for _, ownerID := range r.ownerIDs {
		count, err := r.catalog.Release(ctx, ref, r.ownerType, ownerID)
		if err != nil {
			logger.Warnf(ctx, "Failed to release resource %s from %s %s: %v",
				ref, r.ownerType, ownerID, err)
			return false
		}
		if count >= 0 {
			remaining = count
		}
	}
	// -1 means the reference is not a catalog handle: no claims to account
	// for, so fall back to deleting it as before.
	return remaining <= 0
}

// deleteExtractedImages deletes all extracted image files from storage.
// Standalone function — callable from both entryService and libraryService.
// Errors are logged but do not fail the overall deletion.
//
// releaser may be nil, in which case every file is deleted unconditionally.
func deleteExtractedImages(
	ctx context.Context,
	fileSvc interfaces.FileService,
	releaser *resourceReleaser,
	imageURLs []string,
) {
	if len(imageURLs) == 0 {
		return
	}
	logger.Infof(ctx, "Deleting %d extracted images", len(imageURLs))
	for _, url := range imageURLs {
		if !releaser.deletable(ctx, url) {
			logger.Infof(ctx, "Keeping extracted image %s: another owner may still reference it", url)
			continue
		}
		if err := fileSvc.DeleteFile(ctx, url); err != nil {
			logger.Errorf(ctx, "Failed to delete extracted image %s: %v", url, err)
		}
	}
}

// DeleteEntry deletes a entry entry and all related resources
func (s *entryService) DeleteEntry(ctx context.Context, id string) error {
	// Get the entry entry
	entry, err := s.repo.GetEntryByID(ctx, ctx.Value(types.SpaceIDContextKey).(uint64), id)
	if err != nil {
		return err
	}

	if intake, _ := entry.IntakeMetadata(); entry.SourceKind != "mine" && intake != nil && intake.Decision == "rejected" {
		_, err := s.ReviewEntryIntake(ctx, id, "discard", types.SourceInfo{})
		return err
	}

	// Mark as deleting first to prevent async task conflicts
	// This ensures that any running async tasks will detect the deletion and abort
	originalStatus := entry.ParseStatus
	entry.ParseStatus = types.ParseStatusDeleting
	entry.UpdatedAt = time.Now()
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry failed to mark as deleting")
		// Continue with deletion even if marking fails
	} else {
		logger.Infof(ctx, "Marked entry %s as deleting (previous status: %s)", id, originalStatus)
	}

	// Best-effort: purge any queued downstream tasks for this entry
	// associated with the active document or manual processing attempt.
	// Worker checkpoints already drop them on the floor, but dequeuing
	// here avoids waking workers just to no-op when the parse was still
	// in flight at delete time. No-op in in-memory fixtures and on completed rows
	// (no queued descendants anyway).
	if originalStatus == types.ParseStatusPending ||
		originalStatus == types.ParseStatusProcessing {
		s.dequeueEntryTasks(ctx, id)
	}

	// Resolve file service for this Library before spawning goroutines
	library, _ := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	libraryFileSvc := s.resolveFileService(ctx, library)

	// Collect image URLs before chunks are deleted (ImageInfo references are lost after deletion)
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	chunkImageInfos, err := s.chunkService.GetRepository().ListImageInfoByEntryIDs(ctx, spaceID, []string{id})
	if err != nil {
		logger.Errorf(ctx, "Failed to collect image URLs for cleanup: %v", err)
	}
	var imageInfoStrs []string
	for _, ci := range chunkImageInfos {
		imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
	}
	imageURLs := collectImageURLs(ctx, imageInfoStrs)
	imageURLs = append(imageURLs, processedEntryResources(entry)...)

	wg := errgroup.Group{}
	// Delete entry embeddings from PostgreSQL index.
	// Skip entirely when the entry has no embedding model (disabled indexing):
	// nothing was ever written to the PostgreSQL index, so there is nothing to delete,
	// and GetEmbeddingModel would fail with "model ID cannot be empty".
	if s.db != nil && entry.SourceKind == "mine" {
		wg.Go(func() error {
			return s.db.WithContext(ctx).Table("embeddings").Where("entry_id=?", entry.ID).Delete(nil).Error
		})
	} else if strings.TrimSpace(entry.EmbeddingModelID) != "" {
		wg.Go(func() error {
			retrieveEngine := s.retrieveEngine
			embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, entry.EmbeddingModelID)
			if err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete entry embedding failed")
				return err
			}
			if err := retrieveEngine.DeleteByEntryIDList(ctx, []string{entry.ID}, embeddingModel.GetDimensions(), entry.Type); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete entry embedding failed")
				return err
			}
			return nil
		})
	} else {
		logger.Infof(ctx, "Entry %s has no embedding model, skipping PostgreSQL index cleanup", entry.ID)
	}

	// Delete all chunks associated with this entry
	wg.Go(func() error {
		if err := s.chunkService.DeleteChunksByEntryID(ctx, entry.ID); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete chunks failed")
			return err
		}
		return nil
	})

	if err = wg.Wait(); err != nil {
		return err
	}
	// Delete the entry row FIRST, then drop its physical file. Physical
	// cleanup is deliberately deferred until the row is gone: if any of the
	// index/chunk cleanups above failed we already returned early with the
	// row (and its file) intact, so the queued retry — or a user-triggered
	// reparse — can still read the original file. Deleting the file before the
	// row could leave a "file missing but row present" zombie that can neither be
	// reparsed nor cleanly re-deleted (issue #2192). Orphaning a file after the
	// row is gone is the tolerable failure mode instead.
	if err := s.rememberIngestDiscard(ctx, entry, ingestDiscardReason(ctx)); err != nil {
		return err
	}
	if s.db != nil && entry.SourceKind == "article" {
		if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest_discard", id, map[string]any{"url": entry.Source, "missing": entry.IngestMissing()}, map[string]any{"reason": ingestDiscardReason(ctx)}); err != nil {
			return err
		}
	}
	if s.db != nil && entry.SourceKind == "mine" {
		if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "delete_document", id, map[string]any{"title": entry.Title, "source_kind": "mine", "vault_path": entry.VaultPath}, nil); err != nil {
			return err
		}
	}
	if err := deleteEntryAssociations(ctx, s.db, spaceID, []string{id}); err != nil {
		return err
	}
	if err := s.releaseIngestArticleAssets(ctx, id); err != nil {
		return err
	}
	if err := s.repo.DeleteEntry(ctx, spaceID, id); err != nil {
		return err
	}
	if entry.SourceKind == "mine" {
		if err := s.removeDocumentRecords(ctx, entry); err != nil {
			return err
		}
	}

	// Best-effort physical cleanup. Errors here only leak storage; they must not
	// fail the delete now that the row is already gone.
	if s.vault != nil {
		if err := s.vault.DeleteArticle(ctx, entry); err != nil {
			logger.Errorf(ctx, "DeleteEntry vault cleanup failed: %v", err)
		}
	}
	if entry.FilePath != "" {
		if err := libraryFileSvc.DeleteFile(ctx, entry.FilePath); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete file failed")
		}
	}
	if intake, _ := entry.IntakeMetadata(); intake != nil {
		if intake.OriginalPath != "" && intake.OriginalPath != entry.FilePath {
			if err := libraryFileSvc.DeleteFile(ctx, intake.OriginalPath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete intake original failed")
			}
		}
		imageURLs = append(imageURLs, intake.Images...)
	}
	deleteExtractedImages(ctx, libraryFileSvc, entryResourceOwners(s.resourceCatalog, entry.ID), imageURLs)
	spaceInfo := ctx.Value(types.SpaceInfoContextKey).(*types.Space)
	spaceInfo.StorageUsed -= entry.StorageSize
	if err := s.spaceRepo.AdjustStorageUsed(ctx, spaceInfo.ID, -entry.StorageSize); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry update space storage used failed")
	}
	return nil
}

type entryVectorDeleteGroup struct {
	EmbeddingModelID string
	Type             string
	EntryIDs         []string
}

func buildEntryVectorDeleteGroups(
	entries []*types.Entry,
) []entryVectorDeleteGroup {
	type groupKey struct {
		EmbeddingModelID string
		Type             string
	}

	grouped := make(map[groupKey][]string)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		key := groupKey{
			EmbeddingModelID: entry.EmbeddingModelID,
			Type:             entry.Type,
		}
		grouped[key] = append(grouped[key], entry.ID)
	}

	groups := make([]entryVectorDeleteGroup, 0, len(grouped))
	for key, entryIDs := range grouped {
		groups = append(groups, entryVectorDeleteGroup{
			EmbeddingModelID: key.EmbeddingModelID,
			Type:             key.Type,
			EntryIDs:         entryIDs,
		})
	}
	return groups
}

// DeleteEntryList deletes a entry entry and all related resources
func (s *entryService) DeleteEntryList(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// 1. Get the entry entry
	spaceInfo := ctx.Value(types.SpaceInfoContextKey).(*types.Space)
	entryList, err := s.repo.GetEntryBatch(ctx, spaceInfo.ID, ids)
	if err != nil {
		return err
	}
	// Rejected intake rows retain their source specifically for the Library creator's
	// decision. The async worker restores the submitting user in its context,
	// so enforce the same boundary here rather than trusting enqueue callers.
	for _, entry := range entryList {
		intake, intakeErr := entry.IntakeMetadata()
		if intakeErr != nil {
			return intakeErr
		}
		if intake == nil || intake.Decision != "rejected" {
			continue
		}
		_, libraryErr := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
		if libraryErr != nil {
			return libraryErr
		}
		if _, ok := types.PrincipalFromContext(ctx); !ok {
			return errors.New("an authenticated user is required to delete rejected intake")
		}
	}

	// Mark all as deleting first to prevent async task conflicts.
	// Remember which entries still had queued / in-flight downstream tasks
	// so we can dequeue them in one pass after marking.
	var inFlightIDs []string
	for _, entry := range entryList {
		prev := entry.ParseStatus
		entry.ParseStatus = types.ParseStatusDeleting
		entry.UpdatedAt = time.Now()
		if err := s.repo.UpdateEntry(ctx, entry); err != nil {
			logger.GetLogger(ctx).WithField("error", err).WithField("entry_id", entry.ID).
				Errorf("DeleteEntryList failed to mark as deleting")
			// Continue with deletion even if marking fails
		}
		if prev == types.ParseStatusPending || prev == types.ParseStatusProcessing {
			inFlightIDs = append(inFlightIDs, entry.ID)
		}
	}
	logger.Infof(ctx, "Marked %d entry entries as deleting", len(entryList))

	// Best-effort dequeue of downstream tasks for in-flight entries.
	// See DeleteEntry for the rationale; loop is per-entry because
	// the inspector only filters by entry_id, not by ID set.
	for _, kid := range inFlightIDs {
		s.dequeueEntryTasks(ctx, kid)
	}

	// Pre-resolve Library metadata and file services so goroutines don't need DB access.
	libraries := make(map[string]*types.Library)
	libraryFileServices := make(map[string]interfaces.FileService)
	for _, entry := range entryList {
		if _, ok := libraryFileServices[entry.LibraryID]; !ok {
			library, _ := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
			libraries[entry.LibraryID] = library
			libraryFileServices[entry.LibraryID] = s.resolveFileService(ctx, library)
		}
	}

	// Collect image URLs before chunks are deleted
	chunkImageInfos, err := s.chunkService.GetRepository().ListImageInfoByEntryIDs(ctx, spaceInfo.ID, ids)
	if err != nil {
		logger.Errorf(ctx, "Failed to collect image URLs for batch cleanup: %v", err)
	}
	entryToLibrary := make(map[string]string)
	libraryImageURLs := make(map[string][]string) // libraryID → []imageURL
	for _, k := range entryList {
		entryToLibrary[k.ID] = k.LibraryID
		libraryImageURLs[k.LibraryID] = append(libraryImageURLs[k.LibraryID], processedEntryResources(k)...)
		if intake, _ := k.IntakeMetadata(); intake != nil {
			libraryImageURLs[k.LibraryID] = append(libraryImageURLs[k.LibraryID], intake.Images...)
		}
	}
	libraryImageInfos := make(map[string][]string) // libraryID → []imageInfo JSON
	for _, ci := range chunkImageInfos {
		libraryID := entryToLibrary[ci.EntryID]
		libraryImageInfos[libraryID] = append(libraryImageInfos[libraryID], ci.ImageInfo)
	}
	for libraryID, infos := range libraryImageInfos {
		libraryImageURLs[libraryID] = append(libraryImageURLs[libraryID], collectImageURLs(ctx, infos)...)
	}
	libraryEntryIDs := make(map[string][]string) // libraryID → entry IDs releasing their claims
	for _, k := range entryList {
		libraryEntryIDs[k.LibraryID] = append(libraryEntryIDs[k.LibraryID], k.ID)
	}

	wg := errgroup.Group{}
	// 2. Delete entry embeddings from PostgreSQL index
	wg.Go(func() error {
		documentIDs := make(map[string]bool)
		for _, entry := range entryList {
			documentIDs[entry.ID] = entry.SourceKind == "mine"
		}
		for _, group := range buildEntryVectorDeleteGroups(entryList) {
			var modelEntries []string
			for _, entryID := range group.EntryIDs {
				if documentIDs[entryID] && s.db != nil {
					if err := s.db.WithContext(ctx).Table("embeddings").Where("entry_id=?", entryID).Delete(nil).Error; err != nil {
						return err
					}
				} else {
					modelEntries = append(modelEntries, entryID)
				}
			}
			if len(modelEntries) == 0 {
				continue
			}
			// Entry with disabled indexing never had embeddings written to the PostgreSQL index,
			// and its EmbeddingModelID is intentionally empty. Skip the whole group
			// to avoid the spurious "model ID cannot be empty" failure.
			if strings.TrimSpace(group.EmbeddingModelID) == "" {
				logger.Infof(ctx, "Skipping PostgreSQL index cleanup for %d entry entries without embedding model", len(group.EntryIDs))
				continue
			}

			retrieveEngine := s.retrieveEngine
			embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, group.EmbeddingModelID)
			if err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry get embedding model failed")
				return err
			}
			if err := retrieveEngine.DeleteByEntryIDList(ctx, modelEntries, embeddingModel.GetDimensions(), group.Type); err != nil {
				logger.GetLogger(ctx).
					WithField("error", err).
					Errorf("DeleteEntry delete entry embedding failed")
				return err
			}
		}
		return nil
	})

	// 4. Delete all chunks associated with this entry
	wg.Go(func() error {
		if err := s.chunkService.DeleteByEntryList(ctx, ids); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete chunks failed")
			return err
		}
		return nil
	})

	if err = wg.Wait(); err != nil {
		return err
	}
	// 6. Delete the entry rows FIRST, then drop their physical files. See
	// DeleteEntry for the rationale: deferring file removal until the rows are
	// gone avoids "file missing but row present" zombies that break reparse /
	// re-delete when an earlier cleanup step failed (issue #2192). A failure below
	// only orphans storage.
	deleteRows := func(svc *entryService) error {
		for _, entry := range entryList {
			if svc.db != nil && entry.SourceKind == "mine" {
				if err := NewOperationLogService(svc.db).RecordTx(ctx, svc.db, "delete_document", entry.ID, map[string]any{"title": entry.Title, "source_kind": "mine", "vault_path": entry.VaultPath}, nil); err != nil {
					return err
				}
			}
			if err := svc.rememberIngestDiscard(ctx, entry, "用户丢弃"); err != nil {
				return err
			}
			if svc.db != nil && entry.SourceKind == "article" {
				if intake, _ := entry.IntakeMetadata(); intake == nil || intake.Decision != "rejected" {
					if err := NewOperationLogService(svc.db).RecordTx(ctx, svc.db, "ingest_discard", entry.ID, map[string]any{"url": entry.Source, "missing": entry.IngestMissing()}, map[string]any{"reason": ingestDiscardReason(ctx)}); err != nil {
						return err
					}
				}
			}
			if intake, _ := entry.IntakeMetadata(); intake != nil && intake.Decision == "rejected" {
				if svc.db == nil {
					continue
				}
				if err := NewOperationLogService(svc.db).RecordTx(ctx, svc.db, "review_discard", entry.ID, intake, map[string]any{"reason": ingestDiscardReason(ctx)}); err != nil {
					return err
				}
			}
			if err := svc.releaseIngestArticleAssets(ctx, entry.ID); err != nil {
				return err
			}
		}
		if err := deleteEntryAssociations(ctx, svc.db, spaceInfo.ID, ids); err != nil {
			return err
		}
		if err := svc.repo.DeleteEntryList(ctx, spaceInfo.ID, ids); err != nil {
			return err
		}
		for _, entry := range entryList {
			if entry.SourceKind == "mine" {
				if err := svc.removeDocumentRecords(ctx, entry); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if s.db == nil {
		if err := deleteRows(s); err != nil {
			return err
		}
	} else if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		copy := *s
		copy.db = tx
		copy.repo = repository.NewEntryRepository(tx)
		return deleteRows(&copy)
	}); err != nil {
		return err
	}

	storageAdjust := int64(0)
	for _, entry := range entryList {
		if s.vault != nil {
			if err := s.vault.DeleteArticle(ctx, entry); err != nil {
				logger.Errorf(ctx, "DeleteEntryList vault cleanup failed: %v", err)
			}
		}
		if entry.FilePath != "" {
			fSvc := libraryFileServices[entry.LibraryID]
			if err := fSvc.DeleteFile(ctx, entry.FilePath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete file failed")
			}
		}
		if intake, _ := entry.IntakeMetadata(); intake != nil && intake.OriginalPath != "" && intake.OriginalPath != entry.FilePath {
			if err := libraryFileServices[entry.LibraryID].DeleteFile(ctx, intake.OriginalPath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry delete intake original failed")
			}
		}
		storageAdjust -= entry.StorageSize
	}
	// Delete extracted images per Library
	for libraryID, urls := range libraryImageURLs {
		fSvc := libraryFileServices[libraryID]
		if fSvc == nil {
			logger.Warnf(ctx, "No file service for Library %s, skipping %d image deletions", libraryID, len(urls))
			continue
		}
		deleteExtractedImages(ctx, fSvc, entryResourceOwners(s.resourceCatalog, libraryEntryIDs[libraryID]...), urls)
	}
	spaceInfo.StorageUsed += storageAdjust
	if err := s.spaceRepo.AdjustStorageUsed(ctx, spaceInfo.ID, storageAdjust); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteEntry update space storage used failed")
	}
	return nil
}

func (s *entryService) cleanupEntryResources(ctx context.Context, entry *types.Entry) error {
	logger.GetLogger(ctx).Infof("Cleaning entry resources before manual update, entry ID: %s", entry.ID)

	var cleanupErr error

	if entry.ParseStatus == types.ManualEntryStatusDraft && entry.StorageSize == 0 {
		// Draft without indexed data, skip cleanup.
		return nil
	}

	spaceInfo := ctx.Value(types.SpaceInfoContextKey).(*types.Space)
	if entry.EmbeddingModelID != "" {
		retrieveEngine := s.retrieveEngine
		embeddingModel, modelErr := s.modelService.GetEmbeddingModel(ctx, entry.EmbeddingModelID)
		if modelErr != nil {
			logger.GetLogger(ctx).WithField("error", modelErr).Error("Failed to get embedding model during cleanup")
			cleanupErr = errors.Join(cleanupErr, modelErr)
		} else {
			if err := retrieveEngine.DeleteByEntryIDList(ctx, []string{entry.ID}, embeddingModel.GetDimensions(), entry.Type); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Error("Failed to delete manual entry index")
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}

	}

	// Collect image URLs before chunks are deleted
	library, _ := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	fileSvc := s.resolveFileService(ctx, library)
	chunkImageInfos, imgErr := s.chunkService.GetRepository().ListImageInfoByEntryIDs(ctx, spaceInfo.ID, []string{entry.ID})
	if imgErr != nil {
		logger.GetLogger(ctx).WithField("error", imgErr).Error("Failed to collect image URLs for cleanup")
		cleanupErr = errors.Join(cleanupErr, imgErr)
	}
	var imageInfoStrs []string
	for _, ci := range chunkImageInfos {
		imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
	}
	imageURLs := collectImageURLs(ctx, imageInfoStrs)
	retained := map[string]bool{}
	intake, _ := entry.IntakeMetadata()
	var originalImages []string
	if intake != nil {
		originalImages = intake.Images
	}
	for _, ref := range originalImages {
		retained[ref] = true
	}
	deletableImages := imageURLs[:0]
	for _, ref := range imageURLs {
		if !retained[ref] {
			deletableImages = append(deletableImages, ref)
		}
	}
	imageURLs = deletableImages

	if err := s.chunkService.DeleteChunksByEntryID(ctx, entry.ID); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Error("Failed to delete manual entry chunks")
		cleanupErr = errors.Join(cleanupErr, err)
	}

	// Delete extracted images after chunks are deleted. The claims released
	// here are re-taken by triggerManualProcessing, which always runs after
	// this cleanup and re-binds whatever the new body still references.
	deleteExtractedImages(ctx, fileSvc, entryResourceOwners(s.resourceCatalog, entry.ID), imageURLs)

	if entry.StorageSize > 0 {
		spaceInfo.StorageUsed -= entry.StorageSize
		if spaceInfo.StorageUsed < 0 {
			spaceInfo.StorageUsed = 0
		}
		if err := s.spaceRepo.AdjustStorageUsed(ctx, spaceInfo.ID, -entry.StorageSize); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Error("Failed to adjust storage usage during manual cleanup")
			cleanupErr = errors.Join(cleanupErr, err)
		}
		entry.StorageSize = 0
	}

	return cleanupErr
}

// ProcessEntryListDelete handles PostgreSQL queue entry list delete tasks
func (s *entryService) ProcessEntryListDelete(ctx context.Context, t *taskqueue.Task) error {
	var payload types.EntryListDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal entry list delete payload: %v", err)
		return err
	}
	ctx = payload.Initiator.Apply(ctx)

	logger.Infof(ctx, "Processing entry list delete task for %d entry items", len(payload.EntryIDs))

	// Get space info
	space, err := s.spaceRepo.GetSpaceByID(ctx, payload.SpaceID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get space %d: %v", payload.SpaceID, err)
		return err
	}

	// Set context values
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)

	// Delete entry list
	if err := s.DeleteEntryList(ctx, payload.EntryIDs); err != nil {
		logger.Errorf(ctx, "Failed to delete entry list: %v", err)
		return err
	}

	logger.Infof(ctx, "Successfully deleted %d entry items", len(payload.EntryIDs))
	return nil
}

// processedEntryResources includes body files and their images in normal deletion.
func processedEntryResources(k *types.Entry) []string {
	var metadata struct {
		Body   string   `json:"processed_body_path"`
		Images []string `json:"processed_images"`
	}
	if len(k.Metadata) > 0 {
		_ = json.Unmarshal(k.Metadata, &metadata)
	}
	refs := metadata.Images
	if metadata.Body != "" {
		refs = append(refs, metadata.Body)
	}
	return refs
}
