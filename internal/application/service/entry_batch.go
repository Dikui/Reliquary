package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// EntryBatchService is optional so existing narrow entry service clients remain unchanged.
type EntryBatchService interface {
	PreviewEntryBatch(context.Context, types.EntryBatchRequest) (*types.EntryBatchPreview, error)
	SubmitEntryBatch(context.Context, types.EntryBatchRequest) (*types.EntryBatchResult, error)
	GetEntryBatchResult(context.Context, string, int, int) (*types.EntryBatchResult, error)
	RetryEntryBatch(context.Context, string) (*types.EntryBatchResult, error)
	ProcessEntryBatch(context.Context, *taskqueue.Task) error
}

func batchSpace(ctx context.Context) (uint64, error) {
	id, ok := types.SpaceIDFromContext(ctx)
	if !ok || id == 0 {
		return 0, apperrors.NewUnauthorizedError("personal space is required")
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return 0, apperrors.NewUnauthorizedError("authenticated editor required")
	}
	return id, nil
}
func batchFilter(f *types.EntryBatchFilters) (types.EntryListFilter, error) {
	out := types.EntryListFilter{}
	if f == nil {
		return out, nil
	}
	out = types.EntryListFilter{LibraryIDs: f.Libraries, EntityIDs: f.EntityIDs, TopicIDs: f.TopicIDs, IncludeRejected: f.IncludeRejected, Keyword: f.Keyword, FileType: f.FileType, ContentType: f.ContentType, Starred: f.Starred, ParseStatus: f.ParseStatus, Source: f.Source, SourceKind: f.SourceKind}
	for _, v := range []struct {
		raw       string
		target    *time.Time
		date, end bool
	}{{f.PublishedFrom, &out.PublishedFrom, true, false}, {f.PublishedTo, &out.PublishedTo, true, true}, {f.StartTime, &out.UpdatedFrom, false, false}, {f.EndTime, &out.UpdatedTo, false, false}} {
		if v.raw == "" {
			continue
		}
		layout := time.RFC3339
		if v.date {
			layout = "2006-01-02"
		}
		location := time.Local
		if v.date {
			location = time.UTC
		}
		parsed, err := time.ParseInLocation(layout, v.raw, location)
		if err != nil && !v.date {
			for _, candidate := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
				parsed, err = time.ParseInLocation(candidate, v.raw, time.Local)
				if err == nil {
					break
				}
			}
		}
		if err != nil {
			return out, apperrors.NewBadRequestError("invalid selection date")
		}
		if v.end {
			parsed = parsed.AddDate(0, 0, 1)
		}
		*v.target = parsed
	}
	if !out.PublishedFrom.IsZero() && !out.PublishedTo.IsZero() && !out.PublishedFrom.Before(out.PublishedTo) {
		return out, apperrors.NewBadRequestError("invalid publication range")
	}
	if !out.UpdatedFrom.IsZero() && !out.UpdatedTo.IsZero() && out.UpdatedFrom.After(out.UpdatedTo) {
		return out, apperrors.NewBadRequestError("invalid update range")
	}
	return out, nil
}
func validateBatchRequest(req types.EntryBatchRequest) error {
	if (len(req.Selection.IDs) > 0) == (req.Selection.Filters != nil) {
		return apperrors.NewBadRequestError("select either ids or filters")
	}
	switch req.Action {
	case "delete", "reprocess":
	case "star":
		if req.Starred == nil {
			return apperrors.NewBadRequestError("starred is required")
		}
	case "content_type":
		if strings.TrimSpace(req.ContentType) == "" {
			return apperrors.NewBadRequestError("content_type is required")
		}
	case "topics":
		if len(req.TopicIDs) == 0 || (req.TopicMode != "add" && req.TopicMode != "remove") {
			return apperrors.NewBadRequestError("topic_ids and add/remove topic_mode are required")
		}
	default:
		return apperrors.NewBadRequestError("unsupported batch action")
	}
	return nil
}

func (s *entryService) entryBatchDispatcherActive(ctx context.Context, id string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&taskqueue.Job{}).Where("task_type=? AND payload->>'batch_id'=? AND state IN ?", types.TypeEntryBatch, id, []taskqueue.TaskState{taskqueue.TaskStatePending, taskqueue.TaskStateActive, taskqueue.TaskStateDispatched, taskqueue.TaskStateScheduled, taskqueue.TaskStateRetry}).Count(&count).Error
	return count > 0, err
}

func (s *entryService) libraryBatchBusy(ctx context.Context, spaceID uint64, libraryID, excludeBatch string) (bool, error) {
	active := []taskqueue.TaskState{taskqueue.TaskStatePending, taskqueue.TaskStateDispatched, taskqueue.TaskStateActive, taskqueue.TaskStateScheduled, taskqueue.TaskStateRetry}
	var n int64
	err := s.db.WithContext(ctx).Model(&taskqueue.Job{}).Where("state IN ?", active).
		Where(`group_name=? OR object_id IN (SELECT id FROM entries WHERE space_id=? AND library_id=?) OR (task_type=? AND EXISTS (SELECT 1 FROM entries e WHERE e.space_id=? AND e.library_id=? AND jsonb_exists(payload->'entry_ids',e.id)))`, libraryRebuildGroup(libraryID), spaceID, libraryID, types.TypeEntryListReparse, spaceID, libraryID).Count(&n).Error
	if err != nil || n > 0 {
		return n > 0, err
	}
	err = s.db.WithContext(ctx).Model(&types.EntryBatchItem{}).Where("space_id=? AND library_id=? AND batch_id<>? AND status IN ?", spaceID, libraryID, excludeBatch, []string{"pending", "running", "waiting"}).Count(&n).Error
	return n > 0, err
}

func (s *entryService) prepareEntryBatch(ctx context.Context, req types.EntryBatchRequest, lock bool) (*types.EntryBatchPreview, []types.EntryBatchItem, error) {
	spaceID, err := batchSpace(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err = validateBatchRequest(req); err != nil {
		return nil, nil, err
	}
	var entries []*types.Entry
	ids := uniqueTopicIDs(req.Selection.IDs)
	if req.Selection.Filters != nil {
		f, err := batchFilter(req.Selection.Filters)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range f.LibraryIDs {
			l, err := s.libraryService.GetLibraryByID(ctx, id)
			if err != nil {
				return nil, nil, err
			}
			if l.SpaceID != spaceID {
				return nil, nil, apperrors.NewNotFoundError("library not found")
			}
		}
		entries, _, err = s.repo.ListPagedEntryByLibraryID(ctx, spaceID, "", nil, f)
		if err != nil {
			return nil, nil, err
		}
	} else {
		err = s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, ids).Find(&entries).Error
		if err != nil {
			return nil, nil, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	libraries := map[string]bool{}
	for _, entry := range entries {
		libraries[entry.LibraryID] = true
	}
	libraryIDs := []string{}
	for id := range libraries {
		libraryIDs = append(libraryIDs, id)
	}
	sort.Strings(libraryIDs)
	if req.Action == "content_type" && len(libraryIDs) > 1 {
		return nil, nil, apperrors.NewBadRequestError("content type batch requires one library")
	}
	busy := map[string]bool{}
	for _, id := range libraryIDs {
		if lock {
			if err := s.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", libraryRebuildGroup(id)).Error; err != nil {
				return nil, nil, err
			}
		}
		busy[id], err = s.libraryBatchBusy(ctx, spaceID, id, "")
		if err != nil {
			return nil, nil, err
		}
	}
	topics := []types.Topic{}
	if req.Action == "topics" {
		if err := s.db.WithContext(ctx).Where("space_id=? AND id IN ?", spaceID, uniqueTopicIDs(req.TopicIDs)).Find(&topics).Error; err != nil {
			return nil, nil, err
		}
		if len(topics) != len(uniqueTopicIDs(req.TopicIDs)) {
			return nil, nil, apperrors.NewBadRequestError("topic is unavailable in this space")
		}
	}
	preview := &types.EntryBatchPreview{Reasons: []types.EntryBatchReason{}, Samples: []types.EntryBatchSample{}}
	items := []types.EntryBatchItem{}
	reasons := map[string]int{}
	found := map[string]bool{}
	signatures := []any{}
	for _, entry := range entries {
		found[entry.ID] = true
		item := types.EntryBatchItem{EntryID: entry.ID, SpaceID: spaceID, Title: entry.Title, LibraryID: entry.LibraryID, Status: "pending"}
		reason := ""
		blocked := false
		if entry.ParseStatus == types.ParseStatusDeleting {
			reason = "deleting"
		} else if busy[entry.LibraryID] && (req.Action == "review" || req.Action == "reprocess" || req.Action == "content_type") {
			reason = "library_busy"
			blocked = true
		} else if (req.Action == "review" || req.Action == "reprocess" || req.Action == "content_type" || req.Action == "topics") && (entry.ParseStatus == types.ParseStatusPending || entry.ParseStatus == types.ParseStatusProcessing || entry.ParseStatus == types.ParseStatusFinalizing) {
			reason = "processing"
			blocked = true
		}
		if reason == "" {
			switch req.Action {
			case "review", "reprocess":
				library, e := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
				if e != nil {
					return nil, nil, e
				}
				if req.Action == "review" && library.Template == types.TemplateGenerated {
					reason = "generated_library"
				} else if req.Action == "review" && !libraryReviewEligible(entry, "all") {
					reason = "review_ineligible"
				} else if req.Action == "reprocess" {
					eligible, e := libraryRebuildEligible(entry, "all")
					if e != nil {
						return nil, nil, e
					}
					if !eligible {
						reason = "reprocess_ineligible"
					}
				}
				if reason == "" {
					_, available, e := s.reprocessStoredBody(ctx, entry)
					if e != nil || !available {
						reason = "body_unavailable"
					}
				}
			case "content_type":
				library, err := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
				if err != nil {
					return nil, nil, err
				}
				if library.Template == types.TemplateGenerated {
					reason = "generated_library"
					break
				}
				if !isIngestArticle(entry) || entry.ParseStatus != types.ParseStatusCompleted {
					reason = "requires_completed_article"
				} else {
					var n int64
					if err := s.db.WithContext(ctx).Model(&types.LibraryContentType{}).Where("library_id=? AND name=? AND enabled=true", entry.LibraryID, req.ContentType).Count(&n).Error; err != nil {
						return nil, nil, err
					}
					if n == 0 {
						return nil, nil, apperrors.NewBadRequestError("content type must belong to this library")
					}
				}
			case "star":
				if !isIngestArticle(entry) && entry.SourceKind != "report" {
					reason = "unsupported_material"
				}
			case "topics":
				if !isIngestArticle(entry) {
					reason = "unsupported_material"
				}
				for _, topic := range topics {
					if len(topic.LibraryIDs) > 0 {
						applicable := false
						for _, id := range topic.LibraryIDs {
							applicable = applicable || id == entry.LibraryID
						}
						if !applicable {
							reason = "topic_library_mismatch"
						}
					}
				}
			}
		}
		if reason == "" {
			var overlapping int64
			if err := s.db.WithContext(ctx).Model(&types.EntryBatchItem{}).Where("space_id=? AND entry_id=? AND status IN ?", spaceID, entry.ID, []string{"pending", "running", "waiting"}).Count(&overlapping).Error; err != nil {
				return nil, nil, err
			}
			if overlapping > 0 {
				reason = "material_busy"
				blocked = true
			}
		}
		preview.Matched++
		if reason != "" {
			item.Status = "skipped"
			item.Message = reason
			reasons[reason]++
			if blocked {
				preview.Blocked++
			} else {
				preview.Skipped++
			}
		} else {
			preview.Eligible++
			if len(preview.Samples) < 5 {
				preview.Samples = append(preview.Samples, types.EntryBatchSample{ID: entry.ID, Title: entry.Title, LibraryID: entry.LibraryID})
			}
		}
		signatures = append(signatures, []any{entry.ID, entry.UpdatedAt, entry.ParseStatus, item.Message})
		items = append(items, item)
	}
	for _, id := range ids {
		if !found[id] {
			preview.Matched++
			preview.Skipped++
			reasons["unavailable"]++
			items = append(items, types.EntryBatchItem{EntryID: id, SpaceID: spaceID, Status: "skipped", Message: "unavailable"})
			signatures = append(signatures, []any{id, "unavailable"})
		}
	}
	for code, count := range reasons {
		preview.Reasons = append(preview.Reasons, types.EntryBatchReason{Code: code, Count: count})
	}
	sort.Slice(preview.Reasons, func(i, j int) bool { return preview.Reasons[i].Code < preview.Reasons[j].Code })
	req.PreviewFingerprint = ""
	raw, _ := json.Marshal([]any{spaceID, req, signatures, preview.Reasons})
	preview.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(raw))
	return preview, items, nil
}
func (s *entryService) PreviewEntryBatch(ctx context.Context, req types.EntryBatchRequest) (*types.EntryBatchPreview, error) {
	p, _, err := s.prepareEntryBatch(ctx, req, false)
	return p, err
}
func (s *entryService) SubmitEntryBatch(ctx context.Context, req types.EntryBatchRequest) (*types.EntryBatchResult, error) {
	if req.PreviewFingerprint == "" {
		return nil, apperrors.NewBadRequestError("preview_fingerprint is required")
	}
	var id string
	_, err := entryTaskTransaction(ctx, s, func(copy *entryService) (bool, error) {
		preview, items, err := copy.prepareEntryBatch(ctx, req, true)
		if err != nil {
			return false, err
		}
		if preview.Fingerprint != req.PreviewFingerprint {
			return false, apperrors.NewConflictError("selection changed; preview again")
		}
		if preview.Blocked > 0 {
			return false, apperrors.NewConflictError("selected libraries have unfinished work")
		}
		if preview.Eligible == 0 {
			return false, apperrors.NewBadRequestError("no eligible material")
		}
		id = uuid.NewString()
		for i := range items {
			items[i].BatchID = id
		}
		if err := copy.db.CreateInBatches(&items, 200).Error; err != nil {
			return false, err
		}
		payload := types.EntryBatchPayload{SpaceID: types.MustSpaceIDFromContext(ctx), BatchID: id, Request: req, Initiator: types.TaskInitiatorFromContext(ctx)}
		raw, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		_, err = copy.task.Enqueue(taskqueue.NewTask(types.TypeEntryBatch, raw, taskqueue.TaskID(id), taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(time.Hour)))
		return true, err
	})
	if err != nil {
		return nil, err
	}
	return s.GetEntryBatchResult(ctx, id, 1, 20)
}

func (s *entryService) batchHead(ctx context.Context, id string) (*taskqueue.Job, types.EntryBatchPayload, error) {
	spaceID, err := batchSpace(ctx)
	if err != nil {
		return nil, types.EntryBatchPayload{}, err
	}
	var job taskqueue.Job
	err = s.db.WithContext(ctx).Where("id=? AND task_type=? AND (payload->>'space_id')::bigint=?", id, types.TypeEntryBatch, spaceID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, types.EntryBatchPayload{}, apperrors.NewNotFoundError("batch not found")
	}
	if err != nil {
		return nil, types.EntryBatchPayload{}, err
	}
	var payload types.EntryBatchPayload
	err = json.Unmarshal(job.Payload, &payload)
	return &job, payload, err
}
func (s *entryService) GetEntryBatchResult(ctx context.Context, id string, page, pageSize int) (*types.EntryBatchResult, error) {
	_, payload, err := s.batchHead(ctx, id)
	if err != nil {
		return nil, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if err := s.recoverBatchItems(ctx, id); err != nil {
		return nil, err
	}
	var all []types.EntryBatchItem
	if err := s.db.WithContext(ctx).Where("batch_id=?", id).Order("entry_id").Find(&all).Error; err != nil {
		return nil, err
	}
	out := &types.EntryBatchResult{BatchID: id, Action: payload.Request.Action, Status: "completed", Total: len(all), Items: []types.EntryBatchItem{}, Page: page, PageSize: pageSize}
	for _, item := range all {
		switch item.Status {
		case "succeeded":
			out.Succeeded++
		case "warning":
			out.Succeeded++
			out.Warnings++
		case "failed":
			out.Failed++
		case "skipped":
			out.Skipped++
		case "pending":
			out.Pending++
		default:
			out.Running++
		}
	}
	if out.Running > 0 {
		out.Status = "running"
	} else if out.Pending > 0 {
		out.Status = "pending"
	} else if out.Failed > 0 {
		out.Status = "partial"
		if out.Succeeded == 0 {
			out.Status = "failed"
		}
	}
	if page-1 <= len(all)/pageSize {
		start := (page - 1) * pageSize
		end := start + pageSize
		if end > len(all) {
			end = len(all)
		}
		out.Items = all[start:end]
	}
	return out, nil
}

type entryBatchWait struct{}

func (entryBatchWait) Error() string            { return "waiting for batch child operations" }
func (entryBatchWait) DeferredUntil() time.Time { return time.Now().Add(time.Minute) }

func (s *entryService) ProcessEntryBatch(ctx context.Context, t *taskqueue.Task) error {
	var payload types.EntryBatchPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}
	ctx = payload.Initiator.Apply(ctx)
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	space, err := s.spaceRepo.GetSpaceByID(ctx, payload.SpaceID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	var items []types.EntryBatchItem
	if err := s.db.WithContext(ctx).Where("batch_id=? AND status IN ?", payload.BatchID, []string{"pending", "running", "waiting"}).Order("entry_id").Find(&items).Error; err != nil {
		return err
	}
	waiting := false
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.Status == "waiting" {
			done, err := s.reconcileBatchItem(ctx, &item)
			if err != nil {
				return err
			}
			waiting = waiting || !done
			continue
		}
		if err := s.db.WithContext(ctx).Model(&item).Update("status", "running").Error; err != nil {
			return err
		}
		err := s.withTaskObject(ctx, item.EntryID, func(held context.Context) error { return s.executeBatchItem(held, payload, &item) })
		if err != nil {
			item.Status = "failed"
			item.Message = err.Error()
		} else if item.Status == "skipped" {
		} else if item.TaskID != "" {
			item.Status = "waiting"
			waiting = true
		} else if item.Warning != "" {
			item.Status = "warning"
		} else {
			item.Status = "succeeded"
		}
		if err := s.db.WithContext(ctx).Save(&item).Error; err != nil {
			return err
		}
	}
	if waiting {
		return entryBatchWait{}
	}
	return nil
}
func (s *entryService) reconcileBatchItem(ctx context.Context, item *types.EntryBatchItem) (bool, error) {
	var job taskqueue.Job
	err := s.db.WithContext(ctx).Where("id=?", item.TaskID).First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		item.Status, item.Message = "failed", "processing task unavailable; retry this item"
		// A lost child must not leave its entry permanently pending; never alter
		// an entry now owned by another live processing operation.
		live := s.db.Model(&taskqueue.Job{}).Select("1").Where("object_id=? AND task_type IN ? AND state IN ?", item.EntryID,
			[]string{types.TypeManualProcess, types.TypeDocumentProcess}, []taskqueue.TaskState{taskqueue.TaskStatePending, taskqueue.TaskStateDispatched, taskqueue.TaskStateActive, taskqueue.TaskStateScheduled, taskqueue.TaskStateRetry})
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=? AND parse_status IN ? AND NOT EXISTS (?)", item.EntryID, item.SpaceID,
			[]string{types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing}, live).Updates(map[string]any{"parse_status": types.ParseStatusFailed, "error_message": item.Message}).Error; err != nil {
			return false, err
		}
		return true, s.db.WithContext(ctx).Save(item).Error
	}
	if err != nil {
		return false, err
	}
	switch job.State {
	case taskqueue.TaskStateCompleted:
		item.Status = "succeeded"
		var entry types.Entry
		if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", item.EntryID, item.SpaceID).First(&entry).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return false, err
			}
			item.Status = "failed"
			item.Message = "material unavailable after processing"
		} else if entry.ParseStatus == types.ParseStatusPending || entry.ParseStatus == types.ParseStatusProcessing || entry.ParseStatus == types.ParseStatusFinalizing || entry.PendingSubtasksCount > 0 {
			return false, nil
		} else if (entry.ParseStatus == types.ParseStatusFailed || entry.ParseStatus == types.ParseStatusCancelled || entry.ParseStatus == types.ParseStatusDeleting) && !batchReviewCompleted(&entry, job.Payload) {
			item.Status = "failed"
			item.Message = entry.ErrorMessage
			if item.Message == "" {
				item.Message = "processing did not complete"
			}
		}
	case taskqueue.TaskStateArchived, taskqueue.TaskStateCancelled:
		item.Status = "failed"
		item.Message = job.LastError
		if item.Message == "" {
			item.Message = "processing cancelled"
		}
	default:
		return false, nil
	}
	return true, s.db.WithContext(ctx).Save(item).Error
}
func (s *entryService) executeBatchItem(ctx context.Context, p types.EntryBatchPayload, item *types.EntryBatchItem) error {
	req := p.Request
	if req.Action == "review" || req.Action == "reprocess" {
		item.TaskID = fmt.Sprintf("%s:%d:%s", p.BatchID, item.Attempt, item.EntryID)
		var existing int64
		if err := s.db.WithContext(ctx).Model(&taskqueue.Job{}).Where("id=?", item.TaskID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		item.TaskID = ""
	}
	if item.TaskID != "" {
		return nil
	}
	var current types.Entry
	if err := s.db.WithContext(ctx).Where("space_id=? AND id=?", p.SpaceID, item.EntryID).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if req.Action == "delete" {
				return nil
			}
			item.Status = "skipped"
			item.Message = "material unavailable"
			return nil
		}
		return err
	}
	if current.LibraryID != item.LibraryID {
		return errors.New("material library changed")
	}
	if req.Action == "review" && !libraryReviewEligible(&current, "all") {
		item.Status = "skipped"
		item.Message = "review_ineligible"
		return nil
	}
	if req.Action == "reprocess" {
		eligible, err := libraryRebuildEligible(&current, "all")
		if err != nil {
			return err
		}
		if !eligible {
			item.Status = "skipped"
			item.Message = "reprocess_ineligible"
			return nil
		}
	}
	if req.Action == "content_type" {
		library, err := s.libraryService.GetLibraryByID(ctx, current.LibraryID)
		if err != nil {
			return err
		}
		if library.Template == types.TemplateGenerated {
			item.Status = "skipped"
			item.Message = "generated_library"
			return nil
		}
	}
	if req.Action == "topics" && (current.ParseStatus == types.ParseStatusPending || current.ParseStatus == types.ParseStatusProcessing || current.ParseStatus == types.ParseStatusFinalizing) {
		item.Status = "skipped"
		item.Message = "processing"
		return nil
	}
	resumeContentType := false
	if req.Action == "content_type" && item.Attempt > 0 && current.ContentTypeCorrected && current.ContentType == req.ContentType {
		switch current.ParseStatus {
		case types.ParseStatusFailed, types.ParseStatusCancelled, types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing:
			resumeContentType = true
		}
	}
	if req.Action == "content_type" && current.ParseStatus != types.ParseStatusCompleted && !resumeContentType {
		item.Status = "skipped"
		item.Message = "requires_completed_article"
		return nil
	}
	ctx = context.WithValue(ctx, entryBatchChildKey{}, func(tx *gorm.DB, info *taskqueue.TaskInfo) error {
		item.TaskID = info.ID
		return tx.Model(&types.EntryBatchItem{}).Where("batch_id=? AND entry_id=?", item.BatchID, item.EntryID).Updates(map[string]any{"task_id": info.ID, "status": "waiting"}).Error
	})
	switch req.Action {
	case "delete":
		warnings := []string{}
		err := s.DeleteEntry(context.WithValue(ctx, entryDeleteWarningsKey{}, &warnings), item.EntryID)
		item.Warning = strings.Join(warnings, "; ")
		return err
	case "star":
		return s.withTaskObject(ctx, item.EntryID, func(ctx context.Context) error { _, err := s.SetStarred(ctx, item.EntryID, *req.Starred); return err })
	case "content_type":
		if resumeContentType {
			// The classification committed before its extraction child failed.
			_, err := s.ReparseEntry(ctx, item.EntryID, nil)
			return err
		}
		_, err := s.CorrectContentType(ctx, item.EntryID, req.ContentType)
		return err
	case "topics":
		return s.batchTopics(ctx, item, req)
	case "reprocess", "review":
		run := fmt.Sprintf("%s:%d", p.BatchID, item.Attempt)
		child := types.EntryListReparsePayload{SpaceID: p.SpaceID, LibraryID: item.LibraryID, Initiator: p.Initiator}
		var err error
		if req.Action == "review" {
			child.ReviewID = run
			child.ReviewScope = "all"
			err = s.submitLibraryReviewEntry(ctx, child, item.EntryID)
		} else {
			child.RebuildID = run
			child.RebuildScope = "all"
			err = s.submitLibraryRebuildEntry(ctx, child, item.EntryID)
		}
		if err != nil {
			return err
		}
		var n int64
		item.TaskID = run + ":" + item.EntryID
		if err := s.db.WithContext(ctx).Model(&taskqueue.Job{}).Where("id=?", item.TaskID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			item.TaskID = ""
			return errors.New("material is no longer eligible")
		}
		return nil
	}
	return errors.New("unsupported batch action")
}
func (s *entryService) batchTopics(ctx context.Context, item *types.EntryBatchItem, req types.EntryBatchRequest) error {
	return s.withTaskObject(ctx, item.EntryID, func(ctx context.Context) error {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var entry types.Entry
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", item.SpaceID, item.EntryID).First(&entry).Error; err != nil {
				return err
			}
			if !isIngestArticle(&entry) {
				return errors.New("only articles can have topics")
			}
			ids := uniqueTopicIDs(req.TopicIDs)
			var topics []types.Topic
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND id IN ? AND (library_ids='[]'::jsonb OR library_ids @> ?::jsonb)", item.SpaceID, ids, topicLibrariesJSON([]string{entry.LibraryID})).Order("id").Find(&topics).Error; err != nil {
				return err
			}
			if len(topics) != len(ids) {
				return errors.New("topic unavailable for this library")
			}
			var before []types.EntryTopic
			if err := tx.Where("space_id=? AND entry_id=?", item.SpaceID, item.EntryID).Order("topic_id").Find(&before).Error; err != nil {
				return err
			}
			if req.TopicMode == "remove" {
				if err := tx.Where("space_id=? AND entry_id=? AND topic_id IN ?", item.SpaceID, item.EntryID, ids).Delete(&types.EntryTopic{}).Error; err != nil {
					return err
				}
			} else {
				for _, id := range ids {
					link := types.EntryTopic{SpaceID: item.SpaceID, EntryID: item.EntryID, TopicID: id, Source: types.TopicSourceUser}
					if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "entry_id"}, {Name: "topic_id"}}, DoUpdates: clause.Assignments(map[string]any{"source": types.TopicSourceUser})}).Create(&link).Error; err != nil {
						return err
					}
				}
			}
			var after []types.EntryTopic
			if err := tx.Where("space_id=? AND entry_id=?", item.SpaceID, item.EntryID).Order("topic_id").Find(&after).Error; err != nil {
				return err
			}
			return NewOperationLogService(tx).RecordTx(ctx, tx, "correct_topics", item.EntryID, before, after)
		})
		if err == nil && s.vault != nil {
			s.vault.ScheduleRefresh(ctx, item.EntryID)
		}
		return err
	})
}
func (s *entryService) RetryEntryBatch(ctx context.Context, id string) (*types.EntryBatchResult, error) {
	_, payload, err := s.batchHead(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.recoverBatchItems(ctx, id); err != nil {
		return nil, err
	}
	_, err = entryTaskTransaction(ctx, s, func(copy *entryService) (bool, error) {
		var locked taskqueue.Job
		if err := copy.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).First(&locked).Error; err != nil {
			return false, err
		}
		active, err := copy.entryBatchDispatcherActive(ctx, id)
		if err != nil {
			return false, err
		}
		if active {
			return false, apperrors.NewConflictError("batch dispatcher still has unfinished work")
		}
		var unfinished int64
		if err := copy.db.Model(&types.EntryBatchItem{}).Where("batch_id=? AND status IN ?", id, []string{"pending", "running", "waiting"}).Count(&unfinished).Error; err != nil {
			return false, err
		}
		if unfinished > 0 {
			return false, apperrors.NewConflictError("batch still has unfinished work")
		}
		var failed []types.EntryBatchItem
		if err := copy.db.Where("batch_id=? AND status=?", id, "failed").Order("library_id,entry_id").Find(&failed).Error; err != nil {
			return false, err
		}
		if len(failed) == 0 {
			return false, apperrors.NewBadRequestError("batch has no failed items")
		}
		seen := map[string]bool{}
		for _, item := range failed {
			if !seen[item.LibraryID] {
				seen[item.LibraryID] = true
				if err := copy.db.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", libraryRebuildGroup(item.LibraryID)).Error; err != nil {
					return false, err
				}
				busy := false
				var err error
				if payload.Request.Action == "review" || payload.Request.Action == "reprocess" || payload.Request.Action == "content_type" {
					busy, err = copy.libraryBatchBusy(ctx, payload.SpaceID, item.LibraryID, id)
				}
				if err != nil {
					return false, err
				}
				if busy {
					return false, apperrors.NewConflictError("library has unfinished work")
				}
			}
		}
		for _, item := range failed {
			var n int64
			if err := copy.db.Model(&types.EntryBatchItem{}).Where("space_id=? AND entry_id=? AND batch_id<>? AND status IN ?", payload.SpaceID, item.EntryID, id, []string{"pending", "running", "waiting"}).Count(&n).Error; err != nil {
				return false, err
			}
			if n > 0 {
				return false, apperrors.NewConflictError("material has unfinished batch work")
			}
		}
		if err := copy.db.Model(&types.EntryBatchItem{}).Where("batch_id=? AND status=?", id, "failed").Updates(map[string]any{"status": "pending", "message": "", "warning": "", "task_id": "", "attempt": gorm.Expr("attempt+1")}).Error; err != nil {
			return false, err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		_, err = copy.task.Enqueue(taskqueue.NewTask(types.TypeEntryBatch, raw, taskqueue.TaskID(uuid.NewString()), taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(time.Hour)))
		return true, err
	})
	if err != nil {
		return nil, err
	}
	return s.GetEntryBatchResult(ctx, id, 1, 20)
}

// Recover ledger state after a terminal dispatcher failure without interrupting live children.
func (s *entryService) recoverBatchItems(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize recovery with user retries before reading dispatcher or item state.
		var head taskqueue.Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&head, "id=? AND task_type=?", id, types.TypeEntryBatch).Error; err != nil {
			return err
		}
		copy := *s
		copy.db = tx
		active, err := copy.entryBatchDispatcherActive(ctx, id)
		if err != nil || active {
			return err
		}
		var items []types.EntryBatchItem
		if err := tx.Where("batch_id=? AND status IN ?", id, []string{"pending", "running", "waiting"}).Find(&items).Error; err != nil {
			return err
		}
		for _, item := range items {
			if item.TaskID != "" {
				if _, err := copy.reconcileBatchItem(ctx, &item); err != nil {
					return err
				}
				continue
			}
			item.Status = "failed"
			item.Message = "batch dispatcher stopped; retry this item"
			if err := tx.Save(&item).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func batchReviewCompleted(entry *types.Entry, raw json.RawMessage) bool {
	var child types.ManualProcessPayload
	if json.Unmarshal(raw, &child) != nil || child.ReviewID == "" {
		return false
	}
	var metadata struct {
		Completed string `json:"library_review_completed"`
	}
	return json.Unmarshal(entry.Metadata, &metadata) == nil && metadata.Completed == child.ReviewID
}

func (s *entryService) FailEntryBatch(ctx context.Context, t *taskqueue.Task, _ error) error {
	var payload types.EntryBatchPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}
	return s.recoverBatchItems(ctx, payload.BatchID)
}
