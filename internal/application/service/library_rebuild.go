package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func libraryRebuildGroup(id string) string { return "library-rebuild:" + id }

type libraryRebuildContextKey struct{}

func libraryRebuildEligible(entry *types.Entry, scope string) (bool, error) {
	if !isIngestArticle(entry) {
		return false, nil
	}
	if entry.ParseStatus != types.ParseStatusFailed && entry.SummaryStatus != types.SummaryStatusFailed && (scope != "all" || (entry.ParseStatus != types.ParseStatusCompleted && entry.ParseStatus != types.ParseStatusCancelled)) {
		return false, nil
	}
	intake, err := entry.IntakeMetadata()
	if err != nil {
		return false, err
	}
	return intake == nil || intake.Decision == "approved" || (entry.ParseStatus == types.ParseStatusFailed && intake.Decision == "pending"), nil
}

// RebuildLibrary freezes only the membership. Each worker uses the saved library
// settings when it starts; users must keep settings stable during the rebuild.
func (s *entryService) RebuildLibrary(ctx context.Context, libraryID, scope string) (*types.LibraryRebuildResult, error) {
	if scope == "" {
		scope = "failed"
	}
	if scope != "failed" && scope != "all" {
		return nil, apperrors.NewBadRequestError("scope must be failed or all")
	}
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.LibraryRebuildResult, error) {
			return copy.RebuildLibrary(ctx, libraryID, scope)
		})
	}
	spaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || spaceID == 0 {
		return nil, apperrors.NewUnauthorizedError("personal space is required")
	}
	library, err := s.libraryService.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if library.SpaceID != spaceID {
		return nil, apperrors.NewNotFoundError("library not found")
	}
	if err := s.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", libraryRebuildGroup(libraryID)).Error; err != nil {
		return nil, err
	}
	busy, err := s.libraryBatchBusy(ctx, spaceID, libraryID, "")
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, apperrors.NewConflictError("This library has unfinished processing tasks; finish or cancel them before rebuilding")
	}
	var entries []types.Entry
	if err := s.db.WithContext(ctx).Select("id,source_kind,parse_status,summary_status,metadata").Where("space_id=? AND library_id=?", spaceID, libraryID).Order("id").Find(&entries).Error; err != nil {
		return nil, err
	}
	ids := []string{}
	for i := range entries {
		entry := &entries[i]
		if entry.ParseStatus == types.ParseStatusPending || entry.ParseStatus == types.ParseStatusProcessing || entry.ParseStatus == types.ParseStatusFinalizing || entry.ParseStatus == types.ParseStatusDeleting {
			return nil, apperrors.NewConflictError("This library has unfinished processing tasks; finish or cancel them before rebuilding")
		}
		eligible, err := libraryRebuildEligible(entry, scope)
		if err != nil {
			return nil, err
		}
		if eligible {
			ids = append(ids, entry.ID)
		}
	}
	if len(ids) == 0 {
		return nil, apperrors.NewBadRequestError("This library has no documents eligible for the selected rebuild scope")
	}
	runID := uuid.NewString()
	payload, err := json.Marshal(types.EntryListReparsePayload{SpaceID: spaceID, LibraryID: libraryID, EntryIDs: ids, RebuildID: runID, RebuildScope: scope, Initiator: types.TaskInitiatorFromContext(ctx)})
	if err != nil {
		return nil, err
	}
	info, err := s.task.Enqueue(taskqueue.NewTask(types.TypeEntryListReparse, payload,
		taskqueue.TaskID(runID), taskqueue.Group(libraryRebuildGroup(libraryID)), taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(time.Hour)))
	if err != nil {
		return nil, err
	}
	return &types.LibraryRebuildResult{TaskID: info.ID, Total: len(ids)}, nil
}

// The existing durable child task doubles as the submission checkpoint. Keeping
// its ID stable also protects completed children when the dispatcher is retried.
func (s *entryService) submitLibraryRebuildEntry(ctx context.Context, payload types.EntryListReparsePayload, id string) error {
	taskID := payload.RebuildID + ":" + id
	var submitted int64
	if err := s.db.WithContext(ctx).Model(&taskqueue.Job{}).Where("id=?", taskID).Count(&submitted).Error; err != nil || submitted > 0 {
		return err
	}
	return s.withTaskObject(ctx, id, func(ctx context.Context) error {
		_, err := entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			var entry types.Entry
			if err := copy.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=? AND library_id=?", id, payload.SpaceID, payload.LibraryID).First(&entry).Error; err != nil {
				return nil, err
			}
			if err := copy.db.Model(&taskqueue.Job{}).Where("id=?", taskID).Count(&submitted).Error; err != nil || submitted > 0 {
				return &entry, err
			}
			eligible, err := libraryRebuildEligible(&entry, payload.RebuildScope)
			if err != nil {
				return nil, err
			}
			if !eligible {
				return &entry, nil
			}
			body, available, err := copy.reprocessStoredBody(ctx, &entry)
			if err != nil {
				return nil, err
			}
			if !available {
				return nil, fmt.Errorf("stored Markdown body is missing")
			}
			if err := advanceSegmentGeneration(&entry, "processing"); err != nil {
				return nil, err
			}
			library, err := copy.libraryService.GetLibraryByID(ctx, payload.LibraryID)
			if err != nil {
				return nil, err
			}
			resetEntryForReparse(&entry, library)
			child := types.ManualProcessPayload{SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, EntryID: entry.ID, Content: body, NeedCleanup: true, RebuildID: payload.RebuildID}
			intake, err := entry.IntakeMetadata()
			if err != nil {
				return nil, err
			}
			if intake != nil && intake.Decision == "pending" {
				child.ReviewID, child.RebuildID, child.NeedCleanup = payload.RebuildID, "", false
				if err := setLibraryReviewMetadata(&entry, "segment_review_generation", payload.RebuildID); err != nil {
					return nil, err
				}
			}
			if err := copy.repo.UpdateEntry(ctx, &entry); err != nil {
				return nil, err
			}
			data, err := json.Marshal(child)
			if err != nil {
				return nil, err
			}
			_, err = copy.task.Enqueue(taskqueue.NewTask(types.TypeManualProcess, data, taskqueue.TaskID(taskID), taskqueue.Group(libraryRebuildGroup(entry.LibraryID)), taskqueue.Queue(types.QueueDefault), taskqueue.MaxRetry(3), taskqueue.Timeout(30*time.Minute)))
			return &entry, err
		})
		return err
	})
}

func (s *entryService) cleanupLibraryRebuild(ctx context.Context, entry *types.Entry) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, entry.SpaceID); err != nil {
			return err
		}
		if err := graph.Lock(tx, entry.SpaceID); err != nil {
			return err
		}
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).First(&current).Error; err != nil {
			return err
		}
		var entityIDs []string
		if err := tx.Model(&types.EntryEntity{}).Where("entry_id=? AND space_id=?", entry.ID, entry.SpaceID).Pluck("entity_id", &entityIDs).Error; err != nil {
			return err
		}
		for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
			if err := tx.Table(table).Where("entry_id=? AND library_id=?", entry.ID, entry.LibraryID).Delete(nil).Error; err != nil {
				return err
			}
		}
		for _, value := range []any{&types.Chunk{}, &types.EntryEntity{}, &types.EntryProcessingOutput{}} {
			if err := tx.Where("entry_id=? AND space_id=?", entry.ID, entry.SpaceID).Delete(value).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("entry_id=? AND space_id=? AND source=?", entry.ID, entry.SpaceID, types.TopicSourceModel).Delete(&types.EntryTopic{}).Error; err != nil {
			return err
		}
		if err := graph.WithdrawTx(tx, []string{entry.ID}); err != nil {
			return err
		}
		if err := pruneUnreferencedEntities(tx, entry.SpaceID, entityIDs); err != nil {
			return err
		}
		if current.StorageSize > 0 {
			if err := repository.NewSpaceRepository(tx).AdjustStorageUsed(ctx, entry.SpaceID, -current.StorageSize); err != nil {
				return err
			}
		}
		entry.StorageSize = 0
		return tx.Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).Update("storage_size", 0).Error
	})
}

func (s *entryService) processLibraryRebuild(ctx context.Context, library *types.Library, entry *types.Entry, body string) error {
	// Do not run ingestion's source normalization or file cleanup. Old per-entry
	// overrides remain saved, but this run uses the current library defaults.
	ctx = context.WithValue(ctx, libraryRebuildContextKey{}, true)
	err := s.processArticleBody(ctx, library, entry, body, nil, ResolveProcessConfig(library, nil))
	if err == nil && s.vault != nil {
		s.vault.ScheduleRefresh(ctx, entry.ID)
	}
	if err != nil {
		s.markArticleProcessingFailed(ctx, entry, err)
		s.failIntake(ctx, entry)
		return err
	}
	if aborted, _ := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
		return nil
	}
	finalizeIndexedEntryState(entry, entry.StorageSize, time.Now())
	return s.repo.UpdateEntry(ctx, entry)
}
