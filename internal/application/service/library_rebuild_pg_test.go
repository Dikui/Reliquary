package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// Use production queue persistence without publishing to a running Redis server.
type libraryRebuildQueue struct {
	db     *gorm.DB
	failID string
}

func (q *libraryRebuildQueue) Enqueue(task *taskqueue.Task, options ...taskqueue.Option) (info *taskqueue.TaskInfo, err error) {
	err = q.db.Transaction(func(tx *gorm.DB) error {
		info, err = q.EnqueueTx(context.Background(), tx, task, options...)
		return err
	})
	return
}
func (q *libraryRebuildQueue) EnqueueTx(ctx context.Context, tx *gorm.DB, task *taskqueue.Task, options ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	var p types.ManualProcessPayload
	_ = json.Unmarshal(task.Payload(), &p)
	if q.failID != "" && p.EntryID == q.failID {
		return nil, errors.New("fixture enqueue failure")
	}
	client := taskqueue.NewClient(tx)
	defer client.Close()
	return client.EnqueueTx(ctx, tx, task, options...)
}

func rebuildTask(t *testing.T, db *gorm.DB, id string) *taskqueue.Task {
	t.Helper()
	var job taskqueue.Job
	require.NoError(t, db.First(&job, "id=?", id).Error)
	return taskqueue.NewTask(job.TaskType, job.Payload)
}

func rebuildAttachments(t *testing.T, root, id string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(filepath.Join(root, "attachments", id), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(data)
		}
		return nil
	}))
	return files
}

func TestLibraryRebuildPreservesSourcesAndJudgments(t *testing.T) {
	h, vault, _ := newArticleImageHarness(t, true)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	output := articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "OldCo", EntityType: "company"}, {Name: "Partner", EntityType: "company"}, {Name: "ObsoleteTool", EntityType: "product"}}, []string{})
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	result["relations"] = []extractedRelation{{Subject: "OldCo", Object: "Partner", Type: "合作", EvidenceIDs: []int{1}}}
	data, err := json.Marshal(result)
	require.NoError(t, err)
	h.processor.output = string(data)
	k := h.upload(t, "rebuild", "# OldCo and Partner\n\n"+strings.Repeat("Measured original observations remain unchanged. ", 35)+"\n\n![chart]("+articleImageDataURI(t, 7)+")")
	originalBody, err := vault.ReadBody(k)
	require.NoError(t, err)
	originalImages := rebuildAttachments(t, vault.root, k.ID)
	require.NotEmpty(t, originalImages)
	userTopic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "User selected"})
	require.NoError(t, err)
	require.NoError(t, NewTopicService(h.db).SetUserEntryTopics(h.ctx, k.ID, []string{userTopic.ID}))
	k.Starred, k.CollectReason, k.ContentTypeCorrected = true, "Keep my decision", true
	require.NoError(t, k.SetProcessOverrides(&types.EntryProcessOverrides{ChunkingConfig: &types.ChunkingConfig{ChunkSize: 10000}}))
	k.ParseStatus = types.ParseStatusFailed // A fresh rebuild must not reuse this completed segment output.
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	require.NoError(t, h.db.Model(k).Updates(map[string]any{"starred": true, "collect_reason": k.CollectReason, "content_type_corrected": true}).Error)
	var oldEntity types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "OldCo").First(&oldEntity).Error)
	var orphan types.Entity
	require.NoError(t, h.db.Where("canonical_name=?", "ObsoleteTool").First(&orphan).Error)
	require.NoError(t, h.db.Create(&types.EntityMergeSuggestion{ID: uuid.NewString(), SpaceID: k.SpaceID, EntryID: k.ID, NewEntityID: orphan.ID, CandidateEntityID: oldEntity.ID, Status: "pending"}).Error)
	var oldRelation types.Relation
	require.NoError(t, h.db.Where("library_id=?", k.LibraryID).First(&oldRelation).Error)
	otherLibrary := *h.library
	otherLibrary.ID, otherLibrary.SystemRole, otherLibrary.Directory = "other-library", "", "other-library"
	require.NoError(t, h.db.Omit("ContentTypes", "RelationTypes").Create(&otherLibrary).Error)
	other := &types.Entry{ID: uuid.NewString(), SpaceID: k.SpaceID, LibraryID: otherLibrary.ID, Type: "manual", SourceKind: "article", ParseStatus: types.ParseStatusCompleted, VaultBody: "Other source"}
	require.NoError(t, h.db.Create(other).Error)
	var shared []types.Entity
	require.NoError(t, h.db.Where("canonical_name IN ?", []string{"OldCo", "Partner"}).Find(&shared).Error)
	for _, entity := range shared {
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: k.SpaceID, EntryID: other.ID, EntityID: entity.ID}).Error)
	}
	completed := &types.Entry{ID: uuid.NewString(), SpaceID: k.SpaceID, LibraryID: k.LibraryID, SourceKind: "article", ParseStatus: types.ParseStatusCompleted, VaultBody: "Already completed source"}
	require.NoError(t, h.db.Create(completed).Error)
	cancelled := &types.Entry{ID: uuid.NewString(), SpaceID: k.SpaceID, LibraryID: k.LibraryID, SourceKind: "article", ParseStatus: types.ParseStatusCancelled, VaultBody: "Cancelled source"}
	require.NoError(t, h.db.Create(cancelled).Error)
	otherRelation := oldRelation
	otherRelation.ID, otherRelation.LibraryID = uuid.NewString(), otherLibrary.ID
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: otherLibrary.ID, Name: oldRelation.RelationType, Enabled: true}).Error)
	require.NoError(t, h.db.Create(&otherRelation).Error)
	require.NoError(t, h.db.Create(&types.RelationSource{RelationID: otherRelation.ID, EntryID: other.ID, Evidence: []types.RelationEvidence{}}).Error)
	suppression := &types.RelationSuppression{SpaceID: k.SpaceID, LibraryID: k.LibraryID, SubjectID: oldRelation.SubjectID, ObjectID: oldRelation.ObjectID, RelationType: "投资", Actor: "user"}
	require.NoError(t, h.db.Create(&types.LibraryRelationType{LibraryID: k.LibraryID, Name: suppression.RelationType, Enabled: true}).Error)
	require.NoError(t, h.db.Create(suppression).Error)
	h.library.ChunkingConfig = types.ChunkingConfig{ChunkSize: 100, ChunkOverlap: 0, Separators: []string{"\n\n", " ", ""}}
	require.NoError(t, h.db.Omit("ContentTypes", "RelationTypes").Save(h.library).Error)
	h.processor.output = articleProcessingOutput(t, "other", []types.EntityCandidate{{Name: "NewCo", EntityType: "company"}}, []string{})
	var fresh map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.processor.output), &fresh))
	fresh["summary"].(map[string]any)["overview"] = "New computed summary"
	data, err = json.Marshal(fresh)
	require.NoError(t, err)
	h.processor.output = string(data)
	h.service.task = &libraryRebuildQueue{db: h.db}
	_, err = h.service.RebuildLibrary(h.ctx, k.LibraryID, "invalid")
	require.ErrorContains(t, err, "scope must be failed or all")
	run, err := h.service.RebuildLibrary(h.ctx, k.LibraryID, "")
	require.NoError(t, err)
	require.Equal(t, 1, run.Total)
	_, err = h.service.RebuildLibrary(h.ctx, k.LibraryID, "")
	require.ErrorContains(t, err, "unfinished processing")
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	childID := run.TaskID + ":" + k.ID
	injectedErr := errors.New("fixture graph notification failure")
	const callback = "test:fail_orphan_graph_notification"
	require.NoError(t, h.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*types.GraphPendingSync); ok && row.EntityID == orphan.ID && row.Deleted {
			tx.AddError(injectedErr)
		}
	}))
	require.ErrorIs(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, childID)), injectedErr)
	require.NoError(t, h.db.Callback().Create().Remove(callback))
	require.NoError(t, h.db.First(&types.Entity{}, "id=?", orphan.ID).Error)
	require.NoError(t, h.db.Where("entity_id=?", orphan.ID).First(&types.EntityAlias{}).Error)
	require.NoError(t, h.db.Where("entity_id=? AND entry_id=?", orphan.ID, k.ID).First(&types.EntryEntity{}).Error)
	require.NoError(t, h.db.Where("new_entity_id=?", orphan.ID).First(&types.EntityMergeSuggestion{}).Error)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, childID)))
	finished := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, finished.ParseStatus)
	require.Equal(t, "New computed summary", finished.StructuredSummary.Overview)
	require.Equal(t, k.ContentType, finished.ContentType)
	require.True(t, finished.ContentTypeCorrected)
	require.True(t, finished.Starred)
	require.Equal(t, k.CollectReason, finished.CollectReason)
	require.Equal(t, k.Source, finished.Source)
	require.Equal(t, k.VaultPath, finished.VaultPath)
	keptOverrides, err := finished.ProcessOverrides()
	require.NoError(t, err)
	require.Equal(t, 10000, keptOverrides.ChunkingConfig.ChunkSize)
	afterBody, err := vault.ReadBody(finished)
	require.NoError(t, err)
	require.Equal(t, originalBody, afterBody)
	require.Equal(t, originalImages, rebuildAttachments(t, vault.root, k.ID))
	var count int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=? AND chunk_type=?", k.ID, types.ChunkTypeText).Count(&count).Error)
	require.Greater(t, count, int64(1), "current library chunk size overrides the saved per-entry value for this run")
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
		require.NoError(t, h.db.Table(table).Where("entry_id=?", k.ID).Count(&count).Error)
		require.Positive(t, count, table)
	}
	require.NoError(t, h.db.Model(&types.EntryEntity{}).Where("entry_id=? AND entity_id=?", k.ID, oldEntity.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Model(&types.RelationSource{}).Where("entry_id=?", k.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Model(&types.RelationSource{}).Where("entry_id=?", other.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Model(&types.EntryTopic{}).Where("entry_id=? AND topic_id=? AND source=?", k.ID, userTopic.ID, types.TopicSourceUser).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var preserved types.Entity
	require.NoError(t, h.db.First(&preserved, "id=?", oldEntity.ID).Error)
	require.Equal(t, oldEntity.Description, preserved.Description)
	require.ErrorIs(t, h.db.First(&types.Entity{}, "id=?", orphan.ID).Error, gorm.ErrRecordNotFound)
	for _, model := range []any{&types.EntityAlias{}, &types.EntryEntity{}} {
		require.NoError(t, h.db.Model(model).Where("entity_id=?", orphan.ID).Count(&count).Error)
		require.Zero(t, count)
	}
	require.NoError(t, h.db.Model(&types.EntityMergeSuggestion{}).Where("new_entity_id=? OR candidate_entity_id=?", orphan.ID, orphan.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, h.db.Model(&types.GraphPendingSync{}).Where("entity_id=? AND deleted=true", orphan.ID).Count(&count).Error)
	require.Positive(t, count, "isolated entity removal must reach the graph replica")
	require.NoError(t, h.db.Model(&types.RelationSuppression{}).Where("library_id=?", k.LibraryID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Redelivering the parent after processing must not reset the completed child.
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	require.Equal(t, types.ParseStatusCompleted, h.article(t, k.ID).ParseStatus)
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("task_type=?", types.TypeManualProcess).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, completed.ID).ParseStatus)
	require.Equal(t, types.ParseStatusCancelled, h.article(t, cancelled.ID).ParseStatus)

	// The explicit full scope includes completed and cancelled sources, and is
	// retained in the parent task so dispatch does not fall back to failed-only.
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("group_name=?", libraryRebuildGroup(k.LibraryID)).Update("state", taskqueue.TaskStateCompleted).Error)
	full, err := h.service.RebuildLibrary(h.ctx, k.LibraryID, "all")
	require.NoError(t, err)
	require.Equal(t, 3, full.Total)
	parent := rebuildTask(t, h.db, full.TaskID)
	var payload types.EntryListReparsePayload
	require.NoError(t, json.Unmarshal(parent.Payload(), &payload))
	require.Equal(t, "all", payload.RebuildScope)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, parent))
	for _, id := range []string{k.ID, completed.ID, cancelled.ID} {
		require.Equal(t, types.ParseStatusPending, h.article(t, id).ParseStatus)
		require.NotNil(t, rebuildTask(t, h.db, full.TaskID+":"+id))
	}
}

func TestLibraryRebuildFailureCanResumeWithoutResubmitting(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	k := h.upload(t, "retry-rebuild", "# Original\n\nOriginal supported measurements.\n\n![chart]("+articleImageDataURI(t, 8)+")")
	body, err := vault.ReadBody(k)
	require.NoError(t, err)
	images := rebuildAttachments(t, vault.root, k.ID)
	require.NoError(t, h.db.Model(k).Update("parse_status", types.ParseStatusFailed).Error)
	q := &libraryRebuildQueue{db: h.db}
	h.service.task = q
	run, err := h.service.RebuildLibrary(h.ctx, k.LibraryID, "")
	require.NoError(t, err)
	parent := rebuildTask(t, h.db, run.TaskID)
	q.failID = k.ID
	require.ErrorContains(t, h.service.ProcessEntryListReparse(h.ctx, parent), "fixture enqueue failure")
	require.Equal(t, types.ParseStatusFailed, h.article(t, k.ID).ParseStatus, "failed enqueue rolls back the entry reset")
	q.failID = ""
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, parent))
	child := rebuildTask(t, h.db, run.TaskID+":"+k.ID)
	generation := string(segmentGeneration(h.article(t, k.ID), "processing"))
	h.processor.output = "invalid model output"
	require.Error(t, h.service.ProcessManualUpdate(h.ctx, child))
	require.Equal(t, types.SummaryStatusFailed, h.article(t, k.ID).SummaryStatus)
	after, err := vault.ReadBody(k)
	require.NoError(t, err)
	require.Equal(t, body, after)
	require.Equal(t, images, rebuildAttachments(t, vault.root, k.ID))
	h.processor.output = validProcessingFixture("event")
	var updated map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.processor.output), &updated))
	updated["summary"].(map[string]any)["overview"] = "Summary after recovery"
	output, err := json.Marshal(updated)
	require.NoError(t, err)
	h.processor.output = string(output)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, parent))
	require.Equal(t, types.SummaryStatusFailed, h.article(t, k.ID).SummaryStatus)
	// A late database-write failure must not leave the entry completed and thereby
	// cause the queue to skip the unfinished tail on recovery.
	const callback = "test:fail_final_rebuild_update"
	require.NoError(t, h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if entry, ok := tx.Statement.Dest.(*types.Entry); ok && entry.ID == k.ID && entry.SummaryStatus == types.SummaryStatusCompleted {
			tx.AddError(errors.New("fixture final persistence failure"))
		}
	}))
	require.Error(t, h.service.ProcessManualUpdate(h.ctx, child))
	require.NotEqual(t, types.SummaryStatusCompleted, h.article(t, k.ID).SummaryStatus)
	require.NoError(t, h.db.Callback().Update().Remove(callback))
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, child))
	require.Equal(t, types.ParseStatusCompleted, h.article(t, k.ID).ParseStatus)
	require.Equal(t, generation, string(segmentGeneration(h.article(t, k.ID), "processing")))
	require.Equal(t, images, rebuildAttachments(t, vault.root, k.ID))
}

func TestLibraryRebuildPreservesMissingImages(t *testing.T) {
	h, vault := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	body := "# Original observations\n\nThe experiment reports complete instrument measurements.\n\n![chart](https://example.org/missing-chart.png)"
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{
		Library: "official", URL: "https://example.org/rebuild-missing-images",
		Acquisition: "verbatim", Publisher: "Fixture Journal", Content: body,
	})
	require.NoError(t, err)
	h.drain(t)
	before := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, before.ParseStatus)
	original, err := vault.ReadBody(before)
	require.NoError(t, err)
	processingCalls := len(h.processor.requests)
	intake, err := before.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision = "pending"
	require.NoError(t, before.SetIntakeMetadata(intake))
	before.ParseStatus = types.ParseStatusFailed
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, before))
	h.processor.stages = nil

	h.service.task = &libraryRebuildQueue{db: h.db}
	run, err := h.service.RebuildLibrary(h.ctx, before.LibraryID, "failed")
	require.NoError(t, err)
	require.Equal(t, 1, run.Total)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, run.TaskID+":"+before.ID)))

	after := h.article(t, before.ID)
	require.Equal(t, types.ParseStatusCompleted, after.ParseStatus)
	require.Greater(t, len(h.processor.requests), processingCalls)
	require.NotContains(t, h.processor.stages, "intake_review", "rebuilding does not restore the removed model review gate")
	rebuilt, err := vault.ReadBody(after)
	require.NoError(t, err)
	require.Equal(t, original, rebuilt)
	require.Zero(t, transport.calls.Load())

	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("group_name=?", libraryRebuildGroup(before.LibraryID)).Update("state", taskqueue.TaskStateCompleted).Error)
	full, err := h.service.RebuildLibrary(h.ctx, before.LibraryID, "all")
	require.NoError(t, err)
	require.Equal(t, 1, full.Total, "full rebuild includes completed material with missing images")
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, full.TaskID)))
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, full.TaskID+":"+before.ID)))
	completed := h.article(t, before.ID)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	rebuilt, err = vault.ReadBody(completed)
	require.NoError(t, err)
	require.Equal(t, original, rebuilt)
	require.Zero(t, transport.calls.Load())
}

func queuedLegacyLibraryReview(t *testing.T, h *articleProcessingHarness, scope string, ids ...string) *types.LibraryRebuildResult {
	t.Helper()
	runID := uuid.NewString()
	data, err := json.Marshal(types.EntryListReparsePayload{SpaceID: h.library.SpaceID, LibraryID: h.library.ID, EntryIDs: ids, ReviewID: runID, ReviewScope: scope})
	require.NoError(t, err)
	info, err := h.service.task.Enqueue(taskqueue.NewTask(types.TypeEntryListReparse, data, taskqueue.TaskID(runID), taskqueue.Group(libraryRebuildGroup(h.library.ID))))
	require.NoError(t, err)
	return &types.LibraryRebuildResult{TaskID: info.ID, Total: len(ids)}
}

func TestQueuedLegacyLibraryReviewPreservesPendingAndCollectedMaterial(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	keep := h.upload(t, "keep", "# Keep\n\nOriginal measurements.")
	pending := h.upload(t, "legacy-pending", "# Pending\n\nAn unfinished original note.")
	original, err := vault.ReadBody(pending)
	require.NoError(t, err)
	intake, err := pending.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision, intake.Reason = "rejected", "Old value review"
	require.NoError(t, pending.SetIntakeMetadata(intake))
	pending.ParseStatus = types.ParseStatusRejected
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, pending))
	require.NoError(t, h.db.Model(keep).Updates(map[string]any{"starred": true, "collect_reason": "My assessment"}).Error)
	var chunksBefore []string
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", keep.ID).Order("id").Pluck("id", &chunksBefore).Error)
	capture := captureMineReview(h)
	h.processor.reject = true
	h.service.task = &libraryRebuildQueue{db: h.db}
	run := queuedLegacyLibraryReview(t, h, "pending", pending.ID)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, run.TaskID+":"+pending.ID)))
	require.Equal(t, types.ParseStatusCompleted, h.article(t, pending.ID).ParseStatus)
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("group_name=?", libraryRebuildGroup(h.library.ID)).Update("state", taskqueue.TaskStateCompleted).Error)
	run = queuedLegacyLibraryReview(t, h, "all", keep.ID, pending.ID)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	calls := len(h.processor.requests)
	for _, id := range []string{keep.ID, pending.ID} {
		require.NoError(t, h.service.ProcessManualUpdate(h.ctx, rebuildTask(t, h.db, run.TaskID+":"+id)))
	}
	require.Empty(t, capture.reviews)
	require.Len(t, h.processor.requests, calls, "already collected material keeps its derived content")
	var chunksAfter []string
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", keep.ID).Order("id").Pluck("id", &chunksAfter).Error)
	require.Equal(t, chunksBefore, chunksAfter)
	saved := h.article(t, keep.ID)
	require.True(t, saved.Starred)
	require.Equal(t, "My assessment", saved.CollectReason)
	after, err := vault.ReadBody(h.article(t, pending.ID))
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestQueuedLegacyLibraryReviewPersistenceFailureKeepsOriginalAndCanResume(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	h.service.libraryService = NewLibraryServiceForIngestTest(h)
	entry := h.upload(t, "review-failure", "# Original\n\nOriginal measurements survive a database failure.")
	original, err := vault.ReadBody(entry)
	require.NoError(t, err)
	h.service.task = &libraryRebuildQueue{db: h.db}
	run := queuedLegacyLibraryReview(t, h, "all", entry.ID)
	require.NoError(t, h.service.ProcessEntryListReparse(h.ctx, rebuildTask(t, h.db, run.TaskID)))
	const callback = "test:review_persistence"
	armed := true
	require.NoError(t, h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if current, ok := tx.Statement.Dest.(*types.Entry); ok && current.ID == entry.ID && armed {
			armed = false
			tx.AddError(errors.New("review persistence failure"))
		}
	}))
	t.Cleanup(func() { _ = h.db.Callback().Update().Remove(callback) })
	child := rebuildTask(t, h.db, run.TaskID+":"+entry.ID)
	require.ErrorContains(t, h.service.ProcessManualUpdate(h.ctx, child), "review persistence failure")
	after, err := vault.ReadBody(h.article(t, entry.ID))
	require.NoError(t, err)
	require.Equal(t, original, after)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, child))
	require.Equal(t, types.ParseStatusCompleted, h.article(t, entry.ID).ParseStatus)
}
