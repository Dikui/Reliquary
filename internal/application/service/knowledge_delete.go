package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"golang.org/x/sync/errgroup"

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

// knowledgeResourceOwners builds the releaser for a set of knowledge entries
// being deleted. A nil catalog (no resource registry) yields nil, which
// deleteExtractedImages treats as "delete unconditionally", i.e. the behaviour
// from before bindings were tracked.
func knowledgeResourceOwners(catalog interfaces.ResourceCatalog, knowledgeIDs ...string) *resourceReleaser {
	if catalog == nil || len(knowledgeIDs) == 0 {
		return nil
	}
	return &resourceReleaser{catalog: catalog, ownerType: types.ResourceOwnerKnowledge, ownerIDs: knowledgeIDs}
}

// resourceReleaser decides whether a stored file's bytes may be deleted along
// with the domain object that referenced them.
//
// A file can be claimed by more than one owner — an answer saved into the
// knowledge base shares the very blob the chat message still shows, and two
// knowledge entries can share an image. Deleting one owner must drop only that
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
// wrong the other way is an image vanishing from a conversation or document
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
// Standalone function — callable from both knowledgeService and knowledgeBaseService.
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

// DeleteKnowledge deletes a knowledge entry and all related resources
func (s *knowledgeService) DeleteKnowledge(ctx context.Context, id string) error {
	// Get the knowledge entry
	knowledge, err := s.repo.GetKnowledgeByID(ctx, ctx.Value(types.TenantIDContextKey).(uint64), id)
	if err != nil {
		return err
	}

	if intake, _ := knowledge.IntakeMetadata(); intake != nil && intake.Decision == "rejected" {
		_, err := s.ReviewKnowledgeIntake(ctx, id, "discard", types.SourceInfo{})
		return err
	}

	// Mark as deleting first to prevent async task conflicts
	// This ensures that any running async tasks will detect the deletion and abort
	originalStatus := knowledge.ParseStatus
	knowledge.ParseStatus = types.ParseStatusDeleting
	knowledge.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge failed to mark as deleting")
		// Continue with deletion even if marking fails
	} else {
		logger.Infof(ctx, "Marked knowledge %s as deleting (previous status: %s)", id, originalStatus)
	}

	// Best-effort: purge any queued downstream tasks for this knowledge
	// (multimodal / post-process / question / summary / graph extract).
	// Worker checkpoints already drop them on the floor, but dequeuing
	// here avoids waking workers just to no-op when the parse was still
	// in flight at delete time. No-op in in-memory fixtures and on completed rows
	// (no queued descendants anyway).
	if originalStatus == types.ParseStatusPending ||
		originalStatus == types.ParseStatusProcessing {
		s.dequeueKnowledgeTasks(ctx, id)
	}

	// Resolve file service for this KB before spawning goroutines
	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	kbFileSvc := s.resolveFileService(ctx, kb)

	// Collect image URLs before chunks are deleted (ImageInfo references are lost after deletion)
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	chunkImageInfos, err := s.chunkService.GetRepository().ListImageInfoByKnowledgeIDs(ctx, tenantID, []string{id})
	if err != nil {
		logger.Errorf(ctx, "Failed to collect image URLs for cleanup: %v", err)
	}
	var imageInfoStrs []string
	for _, ci := range chunkImageInfos {
		imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
	}
	imageURLs := collectImageURLs(ctx, imageInfoStrs)
	historicalImages, err := evidenceImageReferences(ctx, s.db, tenantID, []string{id})
	if err != nil {
		return err
	}
	imageURLs = append(imageURLs, historicalImages...)

	wg := errgroup.Group{}
	// Delete knowledge embeddings from PostgreSQL index.
	// Skip entirely when the knowledge has no embedding model (disabled indexing):
	// nothing was ever written to the PostgreSQL index, so there is nothing to delete,
	// and GetEmbeddingModel would fail with "model ID cannot be empty".
	if strings.TrimSpace(knowledge.EmbeddingModelID) != "" {
		wg.Go(func() error {
			retrieveEngine := s.retrieveEngine
			embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, knowledge.EmbeddingModelID)
			if err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete knowledge embedding failed")
				return err
			}
			if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, embeddingModel.GetDimensions(), knowledge.Type); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete knowledge embedding failed")
				return err
			}
			return nil
		})
	} else {
		logger.Infof(ctx, "Knowledge %s has no embedding model, skipping PostgreSQL index cleanup", knowledge.ID)
	}

	// Delete all chunks associated with this knowledge
	wg.Go(func() error {
		if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete chunks failed")
			return err
		}
		return nil
	})

	if err = wg.Wait(); err != nil {
		return err
	}
	if err := s.repo.DeleteKnowledgeTagRelations(ctx, id); err != nil {
		logger.Warnf(ctx, "Failed to delete tag relations for knowledge %s: %v", id, err)
	}
	// Delete the knowledge row FIRST, then drop its physical file. Physical
	// cleanup is deliberately deferred until the row is gone: if any of the
	// index/chunk/graph cleanups above failed we already returned early with the
	// row (and its file) intact, so the queued retry — or a user-triggered
	// reparse — can still read the original file. Deleting the file before the
	// row could leave a "file missing but row present" zombie that can neither be
	// reparsed nor cleanly re-deleted (issue #2192). Orphaning a file after the
	// row is gone is the tolerable failure mode instead.
	if s.db != nil && s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		if err := NewKnowledgeContextService(s.db, s.modelService).DeleteMessages(ctx, knowledge.KnowledgeBaseID, []string{id}); err != nil {
			return err
		}
	}
	if err := s.repo.DeleteKnowledge(ctx, tenantID, id); err != nil {
		return err
	}

	// Best-effort physical cleanup. Errors here only leak storage; they must not
	// fail the delete now that the row is already gone.
	if knowledge.FilePath != "" {
		if err := kbFileSvc.DeleteFile(ctx, knowledge.FilePath); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete file failed")
		}
	}
	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		if intake.OriginalPath != "" && intake.OriginalPath != knowledge.FilePath {
			if err := kbFileSvc.DeleteFile(ctx, intake.OriginalPath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete intake original failed")
			}
		}
		imageURLs = append(imageURLs, intake.Images...)
	}
	deleteExtractedImages(ctx, kbFileSvc, knowledgeResourceOwners(s.resourceCatalog, knowledge.ID), imageURLs)
	tenantInfo := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	tenantInfo.StorageUsed -= knowledge.StorageSize
	if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantInfo.ID, -knowledge.StorageSize); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge update tenant storage used failed")
	}
	return nil
}

type knowledgeVectorDeleteGroup struct {
	EmbeddingModelID string
	Type             string
	KnowledgeIDs     []string
}

func buildKnowledgeVectorDeleteGroups(
	knowledges []*types.Knowledge,
) []knowledgeVectorDeleteGroup {
	type groupKey struct {
		EmbeddingModelID string
		Type             string
	}

	grouped := make(map[groupKey][]string)
	for _, knowledge := range knowledges {
		if knowledge == nil {
			continue
		}
		key := groupKey{
			EmbeddingModelID: knowledge.EmbeddingModelID,
			Type:             knowledge.Type,
		}
		grouped[key] = append(grouped[key], knowledge.ID)
	}

	groups := make([]knowledgeVectorDeleteGroup, 0, len(grouped))
	for key, knowledgeIDs := range grouped {
		groups = append(groups, knowledgeVectorDeleteGroup{
			EmbeddingModelID: key.EmbeddingModelID,
			Type:             key.Type,
			KnowledgeIDs:     knowledgeIDs,
		})
	}
	return groups
}

// DeleteKnowledgeList deletes a knowledge entry and all related resources
func (s *knowledgeService) DeleteKnowledgeList(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// 1. Get the knowledge entry
	tenantInfo := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	knowledgeList, err := s.repo.GetKnowledgeBatch(ctx, tenantInfo.ID, ids)
	if err != nil {
		return err
	}
	// Rejected intake rows retain their source specifically for the KB creator's
	// decision. The async worker restores the submitting user in its context,
	// so enforce the same boundary here rather than trusting enqueue callers.
	userID, _ := types.UserIDFromContext(ctx)
	for _, knowledge := range knowledgeList {
		intake, intakeErr := knowledge.IntakeMetadata()
		if intakeErr != nil {
			return intakeErr
		}
		if intake == nil || intake.Decision != "rejected" {
			continue
		}
		_, kbErr := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
		if kbErr != nil {
			return kbErr
		}
		if userID == "" || !types.IsAuthenticatedFromContext(ctx) {
			return errors.New("an authenticated user is required to delete rejected intake")
		}
	}

	// Mark all as deleting first to prevent async task conflicts.
	// Remember which entries still had queued / in-flight downstream tasks
	// so we can dequeue them in one pass after marking.
	var inFlightIDs []string
	for _, knowledge := range knowledgeList {
		prev := knowledge.ParseStatus
		knowledge.ParseStatus = types.ParseStatusDeleting
		knowledge.UpdatedAt = time.Now()
		if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
			logger.GetLogger(ctx).WithField("error", err).WithField("knowledge_id", knowledge.ID).
				Errorf("DeleteKnowledgeList failed to mark as deleting")
			// Continue with deletion even if marking fails
		}
		if prev == types.ParseStatusPending || prev == types.ParseStatusProcessing {
			inFlightIDs = append(inFlightIDs, knowledge.ID)
		}
	}
	logger.Infof(ctx, "Marked %d knowledge entries as deleting", len(knowledgeList))

	// Best-effort dequeue of downstream tasks for in-flight entries.
	// See DeleteKnowledge for the rationale; loop is per-knowledge because
	// the inspector only filters by knowledge_id, not by ID set.
	for _, kid := range inFlightIDs {
		s.dequeueKnowledgeTasks(ctx, kid)
	}

	// Pre-resolve KB metadata and file services so goroutines don't need DB access.
	knowledgeBases := make(map[string]*types.KnowledgeBase)
	kbFileServices := make(map[string]interfaces.FileService)
	for _, knowledge := range knowledgeList {
		if _, ok := kbFileServices[knowledge.KnowledgeBaseID]; !ok {
			kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
			knowledgeBases[knowledge.KnowledgeBaseID] = kb
			kbFileServices[knowledge.KnowledgeBaseID] = s.resolveFileService(ctx, kb)
		}
	}

	// Collect image URLs before chunks are deleted
	chunkImageInfos, err := s.chunkService.GetRepository().ListImageInfoByKnowledgeIDs(ctx, tenantInfo.ID, ids)
	if err != nil {
		logger.Errorf(ctx, "Failed to collect image URLs for batch cleanup: %v", err)
	}
	knowledgeToKB := make(map[string]string)
	kbImageURLs := make(map[string][]string) // kbID → []imageURL
	for _, k := range knowledgeList {
		knowledgeToKB[k.ID] = k.KnowledgeBaseID
		historicalImages, err := evidenceImageReferences(ctx, s.db, tenantInfo.ID, []string{k.ID})
		if err != nil {
			return err
		}
		kbImageURLs[k.KnowledgeBaseID] = append(kbImageURLs[k.KnowledgeBaseID], historicalImages...)
		if intake, _ := k.IntakeMetadata(); intake != nil {
			kbImageURLs[k.KnowledgeBaseID] = append(kbImageURLs[k.KnowledgeBaseID], intake.Images...)
		}
	}
	kbImageInfos := make(map[string][]string) // kbID → []imageInfo JSON
	for _, ci := range chunkImageInfos {
		kbID := knowledgeToKB[ci.KnowledgeID]
		kbImageInfos[kbID] = append(kbImageInfos[kbID], ci.ImageInfo)
	}
	for kbID, infos := range kbImageInfos {
		kbImageURLs[kbID] = append(kbImageURLs[kbID], collectImageURLs(ctx, infos)...)
	}
	kbKnowledgeIDs := make(map[string][]string) // kbID → knowledge IDs releasing their claims
	for _, k := range knowledgeList {
		kbKnowledgeIDs[k.KnowledgeBaseID] = append(kbKnowledgeIDs[k.KnowledgeBaseID], k.ID)
	}

	wg := errgroup.Group{}
	// 2. Delete knowledge embeddings from PostgreSQL index
	wg.Go(func() error {
		for _, group := range buildKnowledgeVectorDeleteGroups(knowledgeList) {
			// Knowledge with disabled indexing never had embeddings written to the PostgreSQL index,
			// and its EmbeddingModelID is intentionally empty. Skip the whole group
			// to avoid the spurious "model ID cannot be empty" failure.
			if strings.TrimSpace(group.EmbeddingModelID) == "" {
				logger.Infof(ctx, "Skipping PostgreSQL index cleanup for %d knowledge entries without embedding model", len(group.KnowledgeIDs))
				continue
			}

			retrieveEngine := s.retrieveEngine
			embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, group.EmbeddingModelID)
			if err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge get embedding model failed")
				return err
			}
			if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, group.KnowledgeIDs, embeddingModel.GetDimensions(), group.Type); err != nil {
				logger.GetLogger(ctx).
					WithField("error", err).
					Errorf("DeleteKnowledge delete knowledge embedding failed")
				return err
			}
		}
		return nil
	})

	// 4. Delete all chunks associated with this knowledge
	wg.Go(func() error {
		if err := s.chunkService.DeleteByKnowledgeList(ctx, ids); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete chunks failed")
			return err
		}
		return nil
	})

	if err = wg.Wait(); err != nil {
		return err
	}
	for _, knowledgeID := range ids {
		if err := s.repo.DeleteKnowledgeTagRelations(ctx, knowledgeID); err != nil {
			logger.Warnf(ctx, "Failed to delete tag relations for knowledge %s: %v", knowledgeID, err)
		}
	}
	// 6. Delete the knowledge rows FIRST, then drop their physical files. See
	// DeleteKnowledge for the rationale: deferring file removal until the rows are
	// gone avoids "file missing but row present" zombies that break reparse /
	// re-delete when an earlier cleanup step failed (issue #2192). A failure below
	// only orphans storage.
	if s.db != nil && s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		for kbID, sourceIDs := range kbKnowledgeIDs {
			if err := NewKnowledgeContextService(s.db, s.modelService).DeleteMessages(ctx, kbID, sourceIDs); err != nil {
				return err
			}
		}
	}
	if err := s.repo.DeleteKnowledgeList(ctx, tenantInfo.ID, ids); err != nil {
		return err
	}

	storageAdjust := int64(0)
	for _, knowledge := range knowledgeList {
		if knowledge.FilePath != "" {
			fSvc := kbFileServices[knowledge.KnowledgeBaseID]
			if err := fSvc.DeleteFile(ctx, knowledge.FilePath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete file failed")
			}
		}
		if intake, _ := knowledge.IntakeMetadata(); intake != nil && intake.OriginalPath != "" && intake.OriginalPath != knowledge.FilePath {
			if err := kbFileServices[knowledge.KnowledgeBaseID].DeleteFile(ctx, intake.OriginalPath); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge delete intake original failed")
			}
		}
		storageAdjust -= knowledge.StorageSize
	}
	// Delete extracted images per KB
	for kbID, urls := range kbImageURLs {
		fSvc := kbFileServices[kbID]
		if fSvc == nil {
			logger.Warnf(ctx, "No file service for KB %s, skipping %d image deletions", kbID, len(urls))
			continue
		}
		deleteExtractedImages(ctx, fSvc, knowledgeResourceOwners(s.resourceCatalog, kbKnowledgeIDs[kbID]...), urls)
	}
	tenantInfo.StorageUsed += storageAdjust
	if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantInfo.ID, storageAdjust); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("DeleteKnowledge update tenant storage used failed")
	}
	return nil
}

func (s *knowledgeService) cleanupKnowledgeResources(ctx context.Context, knowledge *types.Knowledge) error {
	logger.GetLogger(ctx).Infof("Cleaning knowledge resources before manual update, knowledge ID: %s", knowledge.ID)

	var cleanupErr error

	if knowledge.ParseStatus == types.ManualKnowledgeStatusDraft && knowledge.StorageSize == 0 {
		// Draft without indexed data, skip cleanup.
		return nil
	}

	tenantInfo := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	if knowledge.EmbeddingModelID != "" {
		retrieveEngine := s.retrieveEngine
		embeddingModel, modelErr := s.modelService.GetEmbeddingModel(ctx, knowledge.EmbeddingModelID)
		if modelErr != nil {
			logger.GetLogger(ctx).WithField("error", modelErr).Error("Failed to get embedding model during cleanup")
			cleanupErr = errors.Join(cleanupErr, modelErr)
		} else {
			if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, embeddingModel.GetDimensions(), knowledge.Type); err != nil {
				logger.GetLogger(ctx).WithField("error", err).Error("Failed to delete manual knowledge index")
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}

	}

	// Collect image URLs before chunks are deleted
	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	fileSvc := s.resolveFileService(ctx, kb)
	chunkImageInfos, imgErr := s.chunkService.GetRepository().ListImageInfoByKnowledgeIDs(ctx, tenantInfo.ID, []string{knowledge.ID})
	if imgErr != nil {
		logger.GetLogger(ctx).WithField("error", imgErr).Error("Failed to collect image URLs for cleanup")
		cleanupErr = errors.Join(cleanupErr, imgErr)
	}
	var imageInfoStrs []string
	for _, ci := range chunkImageInfos {
		imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
	}
	imageURLs := collectImageURLs(ctx, imageInfoStrs)
	historicalImages, err := evidenceImageReferences(ctx, s.db, tenantInfo.ID, []string{knowledge.ID})
	if err != nil {
		return errors.Join(cleanupErr, err)
	}
	retained := map[string]bool{}
	for _, ref := range historicalImages {
		retained[ref] = true
	}
	deletableImages := imageURLs[:0]
	for _, ref := range imageURLs {
		if !retained[ref] {
			deletableImages = append(deletableImages, ref)
		}
	}
	imageURLs = deletableImages

	if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Error("Failed to delete manual knowledge chunks")
		cleanupErr = errors.Join(cleanupErr, err)
	}

	// Delete extracted images after chunks are deleted. The claims released
	// here are re-taken by triggerManualProcessing, which always runs after
	// this cleanup and re-binds whatever the new body still references.
	deleteExtractedImages(ctx, fileSvc, knowledgeResourceOwners(s.resourceCatalog, knowledge.ID), imageURLs)

	if knowledge.StorageSize > 0 {
		tenantInfo.StorageUsed -= knowledge.StorageSize
		if tenantInfo.StorageUsed < 0 {
			tenantInfo.StorageUsed = 0
		}
		if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantInfo.ID, -knowledge.StorageSize); err != nil {
			logger.GetLogger(ctx).WithField("error", err).Error("Failed to adjust storage usage during manual cleanup")
			cleanupErr = errors.Join(cleanupErr, err)
		}
		knowledge.StorageSize = 0
	}

	return cleanupErr
}

// ProcessKnowledgeListDelete handles Asynq knowledge list delete tasks
func (s *knowledgeService) ProcessKnowledgeListDelete(ctx context.Context, t *asynq.Task) error {
	var payload types.KnowledgeListDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal knowledge list delete payload: %v", err)
		return err
	}
	ctx = payload.Initiator.Apply(ctx)

	logger.Infof(ctx, "Processing knowledge list delete task for %d knowledge items", len(payload.KnowledgeIDs))

	// Get tenant info
	tenant, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get tenant %d: %v", payload.TenantID, err)
		return err
	}

	// Set context values
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)

	// Delete knowledge list
	if err := s.DeleteKnowledgeList(ctx, payload.KnowledgeIDs); err != nil {
		logger.Errorf(ctx, "Failed to delete knowledge list: %v", err)
		return err
	}

	logger.Infof(ctx, "Successfully deleted %d knowledge items", len(payload.KnowledgeIDs))
	return nil
}
