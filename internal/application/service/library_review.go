package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	"reliquary/internal/application/repository"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

type libraryReviewContextKey struct{}

func (s *entryService) enqueueRetainedIntakeReview(ctx context.Context, entry *types.Entry, body string, approved bool) error {
	runID := uuid.NewString()
	if err := setLibraryReviewMetadata(entry, "segment_review_generation", runID); err != nil {
		return err
	}
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		return err
	}
	data, err := json.Marshal(types.ManualProcessPayload{SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, EntryID: entry.ID, Content: body, ReviewID: runID, ReviewApproved: approved})
	if err != nil {
		return err
	}
	_, err = s.task.Enqueue(taskqueue.NewTask(types.TypeManualProcess, data, taskqueue.TaskID(runID+":"+entry.ID), taskqueue.Group(libraryRebuildGroup(entry.LibraryID)), taskqueue.Queue(types.QueueDefault), taskqueue.MaxRetry(3), taskqueue.Timeout(30*time.Minute)))
	return err
}

func libraryReviewEligible(entry *types.Entry, scope string) bool {
	if entry.SourceKind != "article" && entry.SourceKind != "mine" {
		return false
	}
	intake, err := entry.IntakeMetadata()
	if err != nil || (intake != nil && (intake.UserRejected || intake.Reason == "Rejected by user")) {
		return false
	}
	if entry.ParseStatus == types.ParseStatusRejected && intake != nil && intake.Decision == "rejected" {
		return true
	}
	return scope == "all" && (intake == nil || intake.Decision == "approved") &&
		(entry.ParseStatus == types.ParseStatusCompleted || entry.ParseStatus == types.ParseStatusFailed || entry.ParseStatus == types.ParseStatusCancelled)
}

func setLibraryReviewMetadata(entry *types.Entry, key, id string) error {
	metadata := map[string]json.RawMessage{}
	if len(entry.Metadata) > 0 {
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return err
		}
	}
	if key == "segment_review_generation" {
		delete(metadata, "intake_reject_reason")
	}
	metadata[key], _ = json.Marshal(id)
	data, err := json.Marshal(metadata)
	entry.Metadata = types.JSON(data)
	return err
}

func (s *entryService) submitLibraryReviewEntry(ctx context.Context, payload types.EntryListReparsePayload, id string) error {
	taskID := payload.ReviewID + ":" + id
	return s.withTaskObject(ctx, id, func(ctx context.Context) error {
		_, err := entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			var submitted int64
			if err := copy.db.Model(&taskqueue.Job{}).Where("id=?", taskID).Count(&submitted).Error; err != nil || submitted > 0 {
				return nil, err
			}
			var entry types.Entry
			if err := copy.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=? AND library_id=?", id, payload.SpaceID, payload.LibraryID).First(&entry).Error; err != nil {
				return nil, err
			}
			if !libraryReviewEligible(&entry, payload.ReviewScope) {
				return &entry, nil
			}
			body, available, err := copy.reprocessStoredBody(ctx, &entry)
			if err != nil {
				return nil, err
			}
			if !available {
				return nil, fmt.Errorf("stored Markdown body is missing; material was kept unchanged")
			}
			child := types.ManualProcessPayload{SpaceID: entry.SpaceID, LibraryID: entry.LibraryID, EntryID: entry.ID, Content: body, ReviewID: payload.ReviewID}
			intake, _ := entry.IntakeMetadata()
			if intake == nil || intake.Decision == "approved" {
				child.ReviewStatus, child.ReviewEnabled = entry.ParseStatus, entry.EnableStatus
			}
			if err := setLibraryReviewMetadata(&entry, "segment_review_generation", payload.ReviewID); err != nil {
				return nil, err
			}
			entry.ParseStatus, entry.EnableStatus, entry.ErrorMessage = types.ParseStatusPending, "disabled", ""
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

func (s *entryService) processLibraryReviewEntry(ctx context.Context, entry *types.Entry, payload types.ManualProcessPayload) (reviewErr error) {
	var metadata struct {
		Completed string `json:"library_review_completed"`
	}
	if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
		return err
	}
	generation, _ := json.Marshal(payload.ReviewID)
	if metadata.Completed == payload.ReviewID || string(segmentGeneration(entry, "review")) != string(generation) || entry.ParseStatus == types.ParseStatusDeleting || entry.ParseStatus == types.ParseStatusCancelled {
		return nil
	}
	defer func() {
		if reviewErr != nil && ctx.Err() == nil {
			_ = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=? AND parse_status NOT IN ?", entry.ID, entry.SpaceID, []string{types.ParseStatusDeleting, types.ParseStatusCancelled}).Updates(map[string]any{
				"parse_status": types.ParseStatusFailed, "enable_status": "disabled", "error_message": reviewErr.Error(),
			}).Error
		}
	}()
	library, err := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	if err != nil {
		return err
	}
	intake, err := entry.IntakeMetadata()
	if err != nil {
		return err
	}
	if intake == nil {
		intake = &types.EntryIntakeMetadata{Source: "library_review"}
		if validIngestURL(entry.Source) {
			intake.SourceInfo.OriginalURL = entry.Source
		}
	}
	intake.Decision, intake.Reason = "pending", ""
	if payload.ReviewApproved {
		intake.Decision = "approved"
	}
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, libraryReviewContextKey{}, true)
	if err := s.approveRetainedIntake(ctx, entry); err != nil {
		return err
	}
	if payload.ReviewStatus != "" {
		entry.ParseStatus, entry.EnableStatus = payload.ReviewStatus, payload.ReviewEnabled
		if err := setLibraryReviewMetadata(entry, "library_review_completed", payload.ReviewID); err != nil {
			return err
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := graph.Lock(tx, entry.SpaceID); err != nil {
				return err
			}
			if err := repository.NewEntryRepository(tx).UpdateEntry(ctx, entry); err != nil {
				return err
			}
			return tx.Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).Update("source_unverified", entry.SourceUnverified).Error
		})
		if err == nil && s.vault != nil {
			s.vault.ScheduleRefresh(ctx, entry.ID)
		}
		return err
	}
	// Newly approved pending material enters the existing source-preserving
	// processing pipeline. Its review checkpoints remain available on failure.
	entry.ParseStatus, entry.EnableStatus = types.ParseStatusProcessing, "disabled"
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).Update("source_unverified", entry.SourceUnverified).Error; err != nil {
		return err
	}
	if err := s.cleanupLibraryRebuild(ctx, entry); err != nil {
		return err
	}
	if err := s.processLibraryRebuild(ctx, library, entry, payload.Content); err != nil {
		return err
	}
	if entry.ParseStatus != types.ParseStatusCompleted {
		return errors.New("review processing did not complete")
	}
	if err := setLibraryReviewMetadata(entry, "library_review_completed", payload.ReviewID); err != nil {
		return err
	}
	return s.repo.UpdateEntry(ctx, entry)
}
