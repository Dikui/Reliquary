package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/queue"
	"reliquary/internal/types"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReportWriteResult separates source persistence from index readiness.
type ReportWriteResult struct {
	*types.Entry
	IndexStatus string `json:"index_status"`
	Existing    bool   `json:"existing"`
	Message     string `json:"message"`
}

func (s *VaultDocumentService) reportIndexManagedByQueue(ctx context.Context, spaceID uint64, path string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Table("task_pending_ops j").
		Joins("JOIN entries e ON e.id=j.payload->>'knowledge_id'").
		Where("e.space_id=? AND e.vault_path=? AND e.deleted_at IS NULL AND j.task_type=? AND j.state IN ?",
			spaceID, path, types.TypeReportIndex, []string{"pending", "dispatched", "active", "scheduled", "retry", "archived"}).Count(&count).Error
	return count > 0, err
}

func (s *VaultDocumentService) reportWriteResult(ctx context.Context, entry *types.Entry, existing bool) (*ReportWriteResult, error) {
	if err := populateEntryLibraryNames(ctx, s.db, []*types.Entry{entry}); err != nil {
		return nil, err
	}
	status := "completed"
	if entry.ParseStatus != types.ParseStatusCompleted {
		var queued int64
		if err := s.db.WithContext(ctx).Model(&queue.Job{}).
			Where("task_type=? AND payload->>'knowledge_id'=? AND state IN ?", types.TypeReportIndex, entry.ID,
				[]string{"pending", "dispatched", "active", "scheduled", "retry"}).Count(&queued).Error; err != nil {
			return nil, err
		}
		status = "failed"
		if queued > 0 {
			status = "pending_retry"
		}
	}
	message := "Report saved. The report cannot be treated as fact."
	if existing {
		message = "Returned the existing report for this idempotency key. The report cannot be treated as fact."
	}
	return &ReportWriteResult{Entry: entry, IndexStatus: status, Existing: existing, Message: message}, nil
}

func (s *VaultDocumentService) CreateReport(ctx context.Context, title, markdown string, relatedIDs []string) (*ReportWriteResult, error) {
	return s.createReport(ctx, nil, title, markdown, relatedIDs, nil)
}

func (s *VaultDocumentService) CreateReportRequest(ctx context.Context, input ReportInput) (*ReportWriteResult, error) {
	return s.createReportInput(ctx, nil, input, nil)
}

func (s *VaultDocumentService) createReport(ctx context.Context, library *types.Library, title, markdown string, relatedIDs []string, graph *bool) (*ReportWriteResult, error) {
	return s.createReportInput(ctx, library, ReportInput{Title: title, Markdown: markdown, RelatedArticleIDs: relatedIDs}, graph)
}

func (s *VaultDocumentService) createReportInput(ctx context.Context, library *types.Library, input ReportInput, graph *bool) (*ReportWriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title, markdown := strings.TrimSpace(input.Title), input.Markdown
	existing := false
	var err error
	if library == nil {
		ctx, library, err = s.scope(ctx)
	}
	if err != nil {
		return nil, err
	}
	if library.Template != types.TemplateGenerated {
		return nil, werrors.NewBadRequestError("write_report requires the AI reports library")
	}
	var entry *types.Entry
	var absolutePath string
	var createdID string
	var jobID string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := maintenanceops.CheckCollectionTx(ctx, tx); err != nil {
			return err
		}
		// The library lock serializes report writes from separate callers.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(library, "id=?", library.ID).Error; err != nil {
			return err
		}
		metadata, err := validateReport(tx, library, input)
		if err != nil {
			return err
		}
		entry, err = findExistingReport(tx, library, input, metadata)
		if err != nil {
			return err
		}
		if entry != nil {
			existing = true
			return nil
		}
		selected := metadata.RelatedArticleIDs
		created := time.Now().UTC()
		id := uuid.NewString()
		createdID = id
		directory, err := vaultLibraryDirectory(tx, library.ID)
		if err != nil {
			return err
		}
		path := vaultEntryPath(directory, title, id)
		encodedMetadata, _ := json.Marshal(metadata)
		entry = &types.Entry{ID: id, SpaceID: library.SpaceID, LibraryID: library.ID, Type: types.EntryTypeManual,
			Graph: graph, SourceKind: "report", Title: title, VaultPath: path, Source: path, VaultBody: markdown,
			FileName: filepath.Base(path), FileType: "md", EnableStatus: "enabled", Channel: types.ChannelAPI,
			Acquisition: "model_written", ParseStatus: types.ParseStatusPending, Metadata: types.JSON(encodedMetadata), CreatedAt: created, UpdatedAt: created}
		vault := &VaultService{db: tx, root: s.root}
		markdown, err = vault.copyArticleImages(ctx, entry, path, markdown, nil)
		if err != nil {
			return err
		}
		entry.VaultBody = markdown
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		if err := tx.First(entry, "id=?", id).Error; err != nil {
			return err
		}
		for _, topicID := range metadata.TopicIDs {
			if err := tx.Create(&types.EntryTopic{SpaceID: library.SpaceID, EntryID: id, TopicID: topicID, Source: types.TopicSourceUser, CreatedAt: created}).Error; err != nil {
				return err
			}
		}
		header, err := vault.articleHeader(ctx, entry)
		if err != nil {
			return err
		}
		content := string(RenderVaultArticleHeader(header)) + markdown
		entry.FileSize, entry.FileHash = int64(len(content)), vaultContentHash(content)
		if err := tx.Model(entry).Updates(map[string]any{"file_size": entry.FileSize, "file_hash": entry.FileHash}).Error; err != nil {
			return err
		}
		pathOnDisk, err := vaultAbsolute(s.root, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(pathOnDisk), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(pathOnDisk, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		absolutePath = pathOnDisk
		_, writeErr := file.WriteString(content)
		if syncErr := file.Sync(); writeErr == nil {
			writeErr = syncErr
		}
		if closeErr := file.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			return writeErr
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "write_report", entry.ID, nil,
			map[string]any{"title": title, "vault_path": path, "related_article_ids": selected, "related_entities": metadata.RelatedEntities, "topic_ids": metadata.TopicIDs, "request_id": metadata.RequestID}); err != nil {
			return err
		}
		payload, _ := json.Marshal(types.ReportIndexPayload{SpaceID: library.SpaceID, LibraryID: library.ID, EntryID: id})
		job, err := queue.NewClient(s.db).EnqueueTx(ctx, tx, queue.NewTask(types.TypeReportIndex, payload),
			queue.Queue(types.QueueMaintenance), queue.MaxRetry(5), queue.Timeout(30*time.Minute))
		if err == nil {
			jobID = job.ID
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		if absolutePath != "" {
			if cleanupErr := os.Remove(absolutePath); cleanupErr != nil {
				return nil, errors.Join(err, fmt.Errorf("remove unsaved report: %w", cleanupErr))
			}
		}
		if createdID != "" {
			attachmentPath, pathErr := vaultAbsolute(s.root, filepath.ToSlash(filepath.Join("attachments", createdID)))
			if pathErr != nil {
				return nil, errors.Join(err, pathErr)
			}
			if cleanupErr := os.RemoveAll(attachmentPath); cleanupErr != nil {
				return nil, errors.Join(err, cleanupErr)
			}
		}
		return nil, err
	}
	if existing {
		return s.reportWriteResult(ctx, entry, true)
	}
	{
		s.observations[entry.VaultPath] = vaultObservation{hash: entry.FileHash, changedAt: entry.CreatedAt.Add(-s.reportStableWait)}
		if indexed, indexErr := s.retryReportIndex(ctx, library, entry); indexErr == nil {
			entry = indexed
			// A worker may already have claimed the durable job. It will observe
			// the completed hash after this method releases the report mutex.
			_ = s.db.WithContext(ctx).Where("id=? AND state IN ?", jobID, []string{"pending", "scheduled"}).Delete(&queue.Job{}).Error
		} else {
			entry.ParseStatus, entry.ErrorMessage = types.ParseStatusFailed, indexErr.Error()
		}
	}
	return s.reportWriteResult(ctx, entry, false)
}

func (s *VaultDocumentService) retryReportIndex(ctx context.Context, library *types.Library, entry *types.Entry) (*types.Entry, error) {
	vault := &VaultService{db: s.db, root: s.root}
	if err := vault.RefreshHeader(ctx, entry.ID); err != nil {
		return nil, err
	}
	path, err := vaultAbsolute(s.root, entry.VaultPath)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var indexed *types.Entry
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactional := &VaultDocumentService{db: tx, models: s.models,
			retrieval: retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))}
		indexed, err = transactional.indexReport(ctx, library, entry.VaultPath, string(body), entry.CreatedAt)
		return err
	})
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, library.SpaceID).
			Updates(map[string]any{"parse_status": types.ParseStatusFailed, "error_message": err.Error()}).Error
	}
	return indexed, err
}

func (s *VaultDocumentService) HandleReportIndex(ctx context.Context, task *queue.Task) error {
	var payload types.ReportIndexPayload
	if err := types.UnmarshalSpacePayload(task.Payload(), &payload); err != nil || payload.SpaceID != types.SingleSpaceID || payload.EntryID == "" {
		return fmt.Errorf("invalid report indexing payload: %w", queue.SkipRetry)
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	var entry types.Entry
	err := s.db.WithContext(ctx).Where("id=? AND space_id=? AND source_kind='report'", payload.EntryID, payload.SpaceID).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	library, err := repository.NewLibraryRepository(s.db).GetLibraryByID(ctx, entry.LibraryID)
	if err != nil {
		return err
	}
	_, err = s.retryReportIndex(ctx, library, &entry)
	return err
}
