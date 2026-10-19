package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm/clause"
	"reliquary/internal/application/graph"
	werrors "reliquary/internal/errors"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// UpdateEntryContent is an explicit replacement, independent of ingest's
// source-based deduplication and append-only source_ref history.
func (s *entryService) UpdateEntryContent(ctx context.Context, id, content, acquisition string) (result *types.IngestResult, err error) {
	ctx, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if s.vault == nil {
		return nil, werrors.NewServiceUnavailableError("file library is unavailable")
	}
	err = s.withTaskObject(ctx, id, func(ctx context.Context) error {
		changes := &ingestFileChanges{}
		ctx = context.WithValue(ctx, ingestFileChangesKey{}, changes)
		result, err = entryTaskTransaction(ctx, s, func(copy *entryService) (*types.IngestResult, error) {
			return copy.updateEntryContent(ctx, id, content, acquisition, keyID, keyName)
		})
		if err != nil {
			return errors.Join(err, changes.rollback(s.vault))
		}
		return changes.finish()
	})
	if err == nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	return result, err
}

func (s *entryService) updateEntryContent(ctx context.Context, id, content, acquisition string, keyID uint64, keyName string) (*types.IngestResult, error) {
	spaceID, _ := types.SpaceIDFromContext(ctx)
	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	// Serialize library writes while allowing graph inserts' FK key-share locks.
	var library types.Library
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id=? AND space_id=?", entry.LibraryID, spaceID).First(&library).Error; err != nil {
		return nil, err
	}
	if err := lockMessageDeduplication(s.db.WithContext(ctx), entry); err != nil {
		return nil, err
	}
	if err := graph.Lock(s.db.WithContext(ctx), spaceID); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(entry).Error; err != nil {
		return nil, err
	}
	if acquisition == "" {
		acquisition = entry.Acquisition
	}
	if err := validateIngestRequest(&types.IngestRequest{Library: library.ID, Content: content, Acquisition: acquisition}); err != nil {
		return nil, err
	}
	if library.Template == types.TemplateGenerated && acquisition != "model_written" {
		return nil, werrors.NewBadRequestError("generated libraries require acquisition model_written")
	}
	if entry.VaultPath == "" {
		return nil, werrors.NewConflictError("资料尚未归档，请先完成审核和处理")
	}
	if entry.VaultBody == content && entry.Acquisition == acquisition {
		return ingestResult(entry, "exists", "正文未变化。", "", ""), nil
	}
	switch entry.ParseStatus {
	case types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing, types.ParseStatusDeleting:
		return nil, werrors.NewConflictError("资料正在处理或删除，请完成或取消后再更新")
	}
	busy, err := s.libraryBatchBusy(ctx, spaceID, library.ID, "")
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, werrors.NewConflictError("所属库有未完成任务，请完成或取消后再更新")
	}
	for _, ref := range articleImagesInOrder(content, nil) {
		if remoteIngestImage(ref) {
			continue
		}
		if !isArticleAttachmentReference(id, entry.VaultPath, ref) {
			return nil, werrors.NewBadRequestError("正文中的图片只支持本资料已有附件或远程图片链接；新图片请标记缺图后通过 ingest 补全")
		}
		image, err := s.vault.ReadImage(entry, ref)
		if err != nil {
			return nil, werrors.NewBadRequestError("正文引用的本地图片不可读，请保留有效附件或通过 ingest 补图")
		}
		if err := image.Close(); err != nil {
			return nil, err
		}
	}
	before := map[string]any{"content_hash": entry.ContentHash, "acquisition": entry.Acquisition}
	if err := s.cleanupLibraryRebuild(ctx, entry); err != nil {
		return nil, err
	}
	change, err := s.vault.beginArticleContentUpdate(entry, content)
	if err != nil {
		return nil, err
	}
	changes := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges)
	changes.supplements = append(changes.supplements, change)
	entry.Acquisition = acquisition
	entry.LastIngestAPIKeyID, entry.LastIngestAPIKeyName = keyID, keyName
	entry.ExternalCompletionAttempted = false
	setMessageFingerprint(entry, content)
	resetEntryForReparse(entry, &library)
	entry.UpdatedAt = time.Now()
	if library.Template == types.TemplateGenerated {
		var report reportMetadata
		if err := json.Unmarshal(entry.Metadata, &report); err != nil {
			return nil, err
		}
		metadata := map[string]json.RawMessage{}
		if err := json.Unmarshal(entry.Metadata, &metadata); err != nil {
			return nil, err
		}
		metadata["report_fingerprint"], _ = json.Marshal(reportInputFingerprint(ReportInput{Title: entry.Title, Markdown: content}, report))
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		entry.Metadata, entry.EnableStatus = types.JSON(encoded), "enabled"
	} else {
		intake, err := entry.IntakeMetadata()
		if err != nil {
			return nil, err
		}
		if intake == nil {
			intake = &types.EntryIntakeMetadata{Source: "ingest", SourceInfo: types.SourceInfo{OriginalURL: entry.Source}}
		}
		intake.Decision, intake.Reason, intake.UserRejected = "pending", "", false
		// Published material reads its authoritative body from the vault. Do not
		// create another upload copy or retain a stale pre-publication body path.
		intake.OriginalPath = ""
		if err := entry.SetIntakeMetadata(intake); err != nil {
			return nil, err
		}
		missing := addRemoteIngestImageMissing(content, nil, keyID, keyName)
		entry.Incomplete = len(missing) > 0
		if err := entry.SetIngestMissing(missing); err != nil {
			return nil, err
		}
		if err := advanceSegmentGeneration(entry, "processing"); err != nil {
			return nil, err
		}
	}
	if library.Template == types.TemplateOwn {
		entry.SourceBodyHash = sourceBodyHash(content)
	}
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateEntryColumns(ctx, id, map[string]any{
		"source_body_hash": entry.SourceBodyHash, "pending_subtasks_count": 0,
		"acquisition": entry.Acquisition, "incomplete": entry.Incomplete,
		"missing_materials": entry.MissingMaterials, "external_completion_attempted": false,
		"last_ingest_api_key_id": keyID, "last_ingest_api_key_name": keyName,
	}); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", id, before, map[string]any{"action": "update_content", "content_hash": entry.ContentHash, "acquisition": acquisition}); err != nil {
		return nil, err
	}
	if library.Template == types.TemplateGenerated {
		payload, _ := json.Marshal(types.ReportIndexPayload{SpaceID: spaceID, LibraryID: library.ID, EntryID: id})
		_, err = s.task.Enqueue(taskqueue.NewTask(types.TypeReportIndex, payload, taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(5), taskqueue.Timeout(30*time.Minute)))
	} else {
		err = s.enqueueRetainedIntakeReview(ctx, entry, content, false)
	}
	if err != nil {
		return nil, err
	}
	entry.LibraryName = library.Name
	return ingestResult(entry, "accepted", "正文已更新，正在重新审核和建立索引。", "", ""), nil
}
