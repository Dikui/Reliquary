package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/service/file"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestEntryBatchFreezesFilteredMembershipAndPreservesOtherTopics(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	h.service.task = &libraryRebuildQueue{db: h.db}
	entity := types.Entity{ID: uuid.NewString(), SpaceID: 1, CanonicalName: "Batch target", NormalizedName: "batch target", EntityType: "company"}
	require.NoError(t, h.db.Create(&entity).Error)
	topic := types.Topic{ID: uuid.NewString(), SpaceID: 1, Name: "Existing topic", LibraryIDs: []string{h.library.ID}}
	extra := types.Topic{ID: uuid.NewString(), SpaceID: 1, Name: "Added topic", LibraryIDs: []string{h.library.ID}}
	require.NoError(t, h.db.Create(&topic).Error)
	require.NoError(t, h.db.Create(&extra).Error)
	seed := func(name string) string {
		entry := types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: h.library.ID, SourceKind: "article", Type: types.EntryTypeManual, Title: name, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", ContentType: "event"}
		require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved", SourceInfo: types.SourceInfo{PublishedAt: "2026-10-18T23:59:59Z"}}))
		require.NoError(t, h.db.Create(&entry).Error)
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: entry.ID, EntityID: entity.ID}).Error)
		require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: entry.ID, TopicID: topic.ID, Source: types.TopicSourceModel, IsHistorical: true}).Error)
		return entry.ID
	}
	first, second := seed("First"), seed("Second")
	starred := true
	request := types.EntryBatchRequest{Action: "star", Starred: &starred, Selection: types.EntryBatchSelection{Filters: &types.EntryBatchFilters{Libraries: []string{h.library.ID}, EntityIDs: []string{entity.ID}, TopicIDs: []string{topic.ID}, PublishedFrom: "2026-10-18", PublishedTo: "2026-10-18"}}}
	preview, err := h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, 2, preview.Eligible, "preview: %+v", preview)
	request.PreviewFingerprint = preview.Fingerprint
	batch, err := h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	_, err = h.service.SubmitEntryBatch(h.ctx, request)
	require.Error(t, err, "an overlapping batch must not reuse the old preview")
	later := seed("Arrived after submission")
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err := h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 1)
	require.NoError(t, err)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 2, result.Succeeded)
	require.Len(t, result.Items, 1)
	result, err = h.service.GetEntryBatchResult(h.ctx, batch.BatchID, int(^uint(0)>>1), 100)
	require.NoError(t, err)
	require.Empty(t, result.Items, "out-of-range page must not overflow into a negative slice")
	require.True(t, h.article(t, first).Starred)
	require.True(t, h.article(t, second).Starred)
	require.False(t, h.article(t, later).Starred)

	// Explicitly adding an existing model topic must protect the user's choice.
	request = types.EntryBatchRequest{Action: "topics", TopicMode: "add", TopicIDs: []string{topic.ID, extra.ID}, Selection: types.EntryBatchSelection{IDs: []string{first}}}
	preview, err = h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	request.PreviewFingerprint = preview.Fingerprint
	batch, err = h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	var links []types.EntryTopic
	require.NoError(t, h.db.Where("entry_id=?", first).Find(&links).Error)
	require.Len(t, links, 2)
	for _, link := range links {
		require.Equal(t, types.TopicSourceUser, link.Source)
		if link.TopicID == topic.ID {
			require.True(t, link.IsHistorical)
		}
	}
	request.TopicMode = "remove"
	request.TopicIDs = []string{extra.ID}
	preview, err = h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	request.PreviewFingerprint = preview.Fingerprint
	batch, err = h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	links = nil
	require.NoError(t, h.db.Where("entry_id=?", first).Find(&links).Error)
	require.Len(t, links, 1)
	require.Equal(t, topic.ID, links[0].TopicID)

	report := types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: h.library.ID, SourceKind: "report", Title: "Batch AI output", ParseStatus: types.ParseStatusCompleted}
	require.NoError(t, h.db.Create(&report).Error)
	request = types.EntryBatchRequest{Action: "star", Starred: &starred, Selection: types.EntryBatchSelection{Filters: &types.EntryBatchFilters{Libraries: []string{h.library.ID}, SourceKind: "report", Starred: new(bool)}}}
	preview, err = h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, preview.Matched, "AI output must remain selectable by its star state")
	request.PreviewFingerprint = preview.Fingerprint
	batch, err = h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	require.True(t, h.article(t, report.ID).Starred)
}

func TestEntryBatchRetriesOnlyFailedItemsAfterChildCompletion(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	first := h.upload(t, "Successful material", "# First\n\nAcme published original technical measurements.")
	second := h.upload(t, "Retry material", "# Second\n\nAcme published another complete technical announcement.")
	queue := &libraryRebuildQueue{db: h.db, failID: second.ID}
	h.service.task = queue
	request := types.EntryBatchRequest{Action: "reprocess", Selection: types.EntryBatchSelection{IDs: []string{first.ID, second.ID}}}
	preview, err := h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, 2, preview.Eligible)
	request.PreviewFingerprint = preview.Fingerprint
	batch, err := h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	var deferred interface{ DeferredUntil() time.Time }
	require.ErrorAs(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)), &deferred)
	result, err := h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, result.Running)
	require.Equal(t, 1, result.Failed)
	var successful types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=? AND entry_id=?", batch.BatchID, first.ID).First(&successful).Error)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, successful.TaskID)))
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", successful.TaskID).Update("state", taskqueue.TaskStateCompleted).Error)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", batch.BatchID).Update("state", taskqueue.TaskStateCompleted).Error)
	completedAt := h.article(t, first.ID).UpdatedAt
	queue.failID = ""
	retried, err := h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.NoError(t, err)
	require.Equal(t, 1, retried.Succeeded)
	require.Equal(t, 1, retried.Pending)
	require.ErrorAs(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)), &deferred)
	var retry types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=? AND entry_id=?", batch.BatchID, second.ID).First(&retry).Error)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, retry.TaskID)))
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", retry.TaskID).Update("state", taskqueue.TaskStateCompleted).Error)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err = h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 2, result.Succeeded)
	require.Zero(t, result.Failed)
	require.True(t, completedAt.Equal(h.article(t, first.ID).UpdatedAt), "a successful material must not be reprocessed by batch retry")
}

func TestEntryBatchDeleteRetriesDocumentCleanupWithoutFalseWarnings(t *testing.T) {
	h, v, _ := newArticleImageHarness(t, true)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	first := h.upload(t, "Delete once", "# First\n\nOriginal measurements.\n\n![chart]("+articleImageDataURI(t, 31)+")")
	second := h.upload(t, "Retry document cleanup", "# Second\n\nOriginal document.\n\n![chart]("+articleImageDataURI(t, 37)+")")
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", second.ID).Update("source_kind", "mine").Error)
	// The same attachment can be referenced by several stored metadata fields.
	for _, entry := range []*types.Entry{first, second} {
		refs := articleImagePaths(entry)
		require.NotEmpty(t, refs)
		imagePath := filepath.Clean(filepath.Join(v.root, filepath.Dir(entry.VaultPath), refs[0]))
		missing := filepath.Join(v.root, "attachments", entry.ID, "already-removed.png")
		intake, err := entry.IntakeMetadata()
		require.NoError(t, err)
		intake.Images = []string{imagePath, imagePath, missing}
		intake.OriginalPath = filepath.Join(v.root, entry.VaultPath)
		require.NoError(t, entry.SetIntakeMetadata(intake))
		var metadata map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(entry.Metadata, &metadata))
		metadata["processed_images"], err = json.Marshal([]string{imagePath, imagePath, missing})
		require.NoError(t, err)
		delete(metadata, "processed_body_path")
		raw, err := json.Marshal(metadata)
		require.NoError(t, err)
		require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", entry.ID).Updates(map[string]any{"metadata": types.JSON(raw), "file_path": filepath.Join(v.root, entry.VaultPath)}).Error)
		imageInfo, err := json.Marshal([]types.ImageInfo{{URL: imagePath}, {URL: imagePath}})
		require.NoError(t, err)
		require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", entry.ID).Update("image_info", string(imageInfo)).Error)
	}
	h.service.fileSvc = file.NewLocalFileService(v.root, "")
	h.service.task = &libraryRebuildQueue{db: h.db}
	failHardDelete := true
	hardDeletes := 0
	callback := "test:batch-document-hard-delete"
	require.NoError(t, h.db.Callback().Delete().Before("gorm:delete").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "entries" && tx.Statement.Unscoped {
			hardDeletes++
			if failHardDelete {
				tx.AddError(errors.New("injected document hard delete failure"))
			}
		}
	}))
	t.Cleanup(func() { h.db.Callback().Delete().Remove(callback) })
	req := types.EntryBatchRequest{Action: "delete", Selection: types.EntryBatchSelection{IDs: []string{first.ID, second.ID}}}
	preview, err := h.service.PreviewEntryBatch(h.ctx, req)
	require.NoError(t, err)
	req.PreviewFingerprint = preview.Fingerprint
	batch, err := h.service.SubmitEntryBatch(h.ctx, req)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err := h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, 1, result.Succeeded)
	if result.Warnings != 0 {
		t.Errorf("already removed or duplicate image references must not warn: %+v", result.Items)
	}
	var visible int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", second.ID).Count(&visible).Error)
	if visible != 1 {
		t.Errorf("failed hard delete must leave document visible for retry, got %d", visible)
	}
	_, err = os.Stat(filepath.Join(v.root, second.VaultPath))
	require.NoError(t, err, "original file remains until record deletion succeeds")
	var succeeded types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=? AND entry_id=?", batch.BatchID, first.ID).First(&succeeded).Error)
	failHardDelete = false
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", batch.BatchID).Update("state", taskqueue.TaskStateCompleted).Error)
	_, err = h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err = h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 2, result.Succeeded)
	require.Zero(t, result.Failed)
	require.Zero(t, result.Warnings)
	var retained int64
	require.NoError(t, h.db.Unscoped().Model(&types.Entry{}).Where("id=?", second.ID).Count(&retained).Error)
	require.Zero(t, retained, "retry must physically remove the document record")
	require.Equal(t, 2, hardDeletes, "one failed hard delete and one successful retry")
	for _, entry := range []*types.Entry{first, second} {
		_, err = os.Stat(filepath.Join(v.root, entry.VaultPath))
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = os.Stat(filepath.Join(v.root, "attachments", entry.ID))
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	var after types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=? AND entry_id=?", batch.BatchID, first.ID).First(&after).Error)
	require.True(t, succeeded.UpdatedAt.Equal(after.UpdatedAt), "successful delete must not execute again on retry")
}

func TestEntryBatchRetriesContentTypeChildrenAndRecoversMissingTask(t *testing.T) {
	h, _ := newVaultArticleHarness(t)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	entry := h.upload(t, "Type retry", "# Acme\n\nA complete original technical announcement with measurements.")
	_, err := h.service.CorrectContentType(h.ctx, entry.ID, "other")
	require.NoError(t, err)
	h.service.task = &libraryRebuildQueue{db: h.db}
	request := types.EntryBatchRequest{Action: "content_type", ContentType: "event", Selection: types.EntryBatchSelection{IDs: []string{entry.ID}}}
	preview, err := h.service.PreviewEntryBatch(h.ctx, request)
	require.NoError(t, err)
	request.PreviewFingerprint = preview.Fingerprint
	batch, err := h.service.SubmitEntryBatch(h.ctx, request)
	require.NoError(t, err)
	var deferred interface{ DeferredUntil() time.Time }
	require.ErrorAs(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)), &deferred)
	var item types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=?", batch.BatchID).First(&item).Error)
	require.NotEmpty(t, item.TaskID)
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id=?", entry.ID).Update("parse_status", types.ParseStatusFailed).Error)
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", item.TaskID).Updates(map[string]any{"state": taskqueue.TaskStateArchived, "last_error": "fixture failed child"}).Error)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))

	// A queued automatic dispatcher retry must not race a user retry.
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", batch.BatchID).Update("state", taskqueue.TaskStateRetry).Error)
	_, err = h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.Error(t, err, "an unfinished dispatcher must be allowed to settle first")
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", batch.BatchID).Update("state", taskqueue.TaskStateArchived).Error)
	_, err = h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.NoError(t, err)
	require.NoError(t, h.service.FailEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID), errors.New("delayed old dispatcher failure")))
	_, err = h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.Error(t, err, "a second retry must not duplicate the pending dispatcher")
	require.ErrorAs(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)), &deferred)
	var retry types.EntryBatchItem
	require.NoError(t, h.db.Where("batch_id=?", batch.BatchID).First(&retry).Error)
	require.Equal(t, "waiting", retry.Status, "a failed type-change child must be re-enqueued, not skipped")
	require.NotEqual(t, item.TaskID, retry.TaskID)

	// A purged child must fail only this item and remain retryable.
	require.NoError(t, h.db.Delete(&taskqueue.Job{}, "id=?", retry.TaskID).Error)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err := h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Contains(t, result.Items[0].Message, "task unavailable")
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("task_type=? AND payload->>'batch_id'=?", types.TypeEntryBatch, batch.BatchID).Update("state", taskqueue.TaskStateArchived).Error)
	_, err = h.service.RetryEntryBatch(h.ctx, batch.BatchID)
	require.NoError(t, err)
	require.ErrorAs(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)), &deferred)
	require.NoError(t, h.db.Where("batch_id=?", batch.BatchID).First(&retry).Error)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, retry.TaskID)))
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("id=?", retry.TaskID).Update("state", taskqueue.TaskStateCompleted).Error)
	require.NoError(t, h.service.ProcessEntryBatch(h.ctx, rebuildTask(t, h.db, batch.BatchID)))
	result, err = h.service.GetEntryBatchResult(h.ctx, batch.BatchID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	require.Zero(t, result.Skipped)
}
