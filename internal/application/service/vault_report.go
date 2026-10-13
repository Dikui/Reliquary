package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

// ReportWriteResult separates source persistence from index readiness.
type ReportWriteResult struct {
	*types.Entry
	IndexStatus string `json:"index_status"`
	Existing    bool   `json:"existing"`
	Message     string `json:"message"`
}

func reportFingerprint(title, markdown string) string {
	input, _ := json.Marshal([]string{strings.TrimSpace(title), markdown})
	return vaultContentHash(string(input))
}

func (s *VaultDocumentService) reportIndexManagedByQueue(ctx context.Context, spaceID uint64, path string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).Table("task_jobs j").
		Joins("JOIN entries e ON e.id=j.payload->>'knowledge_id'").
		Where("e.space_id=? AND e.vault_path=? AND e.deleted_at IS NULL AND j.task_type=? AND j.state IN ?",
			spaceID, path, types.TypeReportIndex, []string{"pending", "active", "scheduled", "retry", "archived"}).Count(&count).Error
	return count > 0, err
}

func (s *VaultDocumentService) reportWriteResult(ctx context.Context, entry *types.Entry, existing bool) (*ReportWriteResult, error) {
	status := "completed"
	if entry.ParseStatus != types.ParseStatusCompleted {
		var queued int64
		if err := s.db.WithContext(ctx).Model(&queue.Job{}).
			Where("task_type=? AND payload->>'knowledge_id'=? AND state IN ?", types.TypeReportIndex, entry.ID,
				[]string{"pending", "active", "scheduled", "retry"}).Count(&queued).Error; err != nil {
			return nil, err
		}
		status = "failed"
		if queued > 0 {
			status = "pending_retry"
		}
	}
	message := "Report saved."
	if existing {
		message = "Returned the existing report with the same title and Markdown."
	}
	return &ReportWriteResult{Entry: entry, IndexStatus: status, Existing: existing, Message: message}, nil
}

// findExistingReport also recognizes reports written before fingerprints were recorded.
func (s *VaultDocumentService) findExistingReport(ctx context.Context, tx *gorm.DB, spaceID uint64, fingerprint string) (*types.Entry, error) {
	var reports []types.Entry
	if err := tx.WithContext(ctx).Where("space_id=? AND source_kind='report' AND (metadata->>'report_fingerprint'=? OR COALESCE(metadata->>'report_fingerprint','')='')", spaceID, fingerprint).
		Order("created_at,id").Find(&reports).Error; err != nil {
		return nil, err
	}
	for i := range reports {
		report := &reports[i]
		var metadata map[string]any
		_ = json.Unmarshal(report.Metadata, &metadata)
		if metadata["report_fingerprint"] == fingerprint {
			return report, nil
		}
		path, err := vaultAbsolute(s.root, report.VaultPath)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		header, body, err := splitVaultHeader(content)
		if err != nil {
			continue
		}
		var fields struct {
			Title string `yaml:"title"`
		}
		if yaml.Unmarshal(header, &fields) == nil && reportFingerprint(fields.Title, string(body)) == fingerprint {
			return report, nil
		}
	}
	return nil, nil
}

func (s *VaultDocumentService) CreateReport(ctx context.Context, title, markdown string, relatedIDs []string) (*ReportWriteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 255 || !utf8.ValidString(title) || strings.TrimSpace(markdown) == "" || !utf8.ValidString(markdown) {
		return nil, werrors.NewBadRequestError("report requires a title of up to 255 characters and valid Markdown")
	}
	ctx, kb, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	fingerprint := reportFingerprint(title, markdown)
	var entry *types.Entry
	var absolutePath string
	var jobID string
	var existing bool
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := maintenanceops.CheckCollectionTx(ctx, tx); err != nil {
			return err
		}
		// The knowledge base lock serializes report writes from separate callers.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(kb, "id=?", kb.ID).Error; err != nil {
			return err
		}
		entry, err = s.findExistingReport(ctx, tx, kb.SpaceID, fingerprint)
		if err != nil || entry != nil {
			existing = entry != nil
			return err
		}
		selected := []string{}
		if len(relatedIDs) > 0 {
			if err := tx.Model(&types.Entry{}).Where("space_id=? AND source_kind='article' AND id IN ?", kb.SpaceID, relatedIDs).
				Order("id").Pluck("id", &selected).Error; err != nil {
				return err
			}
			unique := map[string]bool{}
			for _, id := range relatedIDs {
				unique[id] = true
			}
			if len(selected) != len(unique) {
				return werrors.NewBadRequestError("related_article_ids contains an unavailable article")
			}
		}
		created := time.Now().UTC()
		id := uuid.NewString()
		path := filepath.ToSlash(filepath.Join("reports", created.Format("2006-01"), vaultSlug(title)+"-"+id[:8]+".md"))
		header := struct {
			Title             string   `yaml:"title"`
			CreatedAt         string   `yaml:"created_at"`
			RelatedArticleIDs []string `yaml:"related_article_ids"`
		}{title, created.Format(time.RFC3339Nano), selected}
		raw, err := yaml.Marshal(header)
		if err != nil {
			return err
		}
		content := "---\n" + string(raw) + "---\n" + markdown
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
		metadata, _ := json.Marshal(map[string]any{"related_article_ids": selected, "report_fingerprint": fingerprint})
		entry = &types.Entry{ID: id, SpaceID: kb.SpaceID, KnowledgeBaseID: kb.ID, Type: types.EntryTypeManual,
			SourceKind: "report", Title: vaultDocumentTitle(path, content), VaultPath: path, Source: path,
			FileName: filepath.Base(path), FileType: "md", FileSize: int64(len(content)), FileHash: vaultContentHash(content),
			EnableStatus: "enabled", Channel: types.ChannelAPI, ProcessingMode: types.ProcessingModeFull,
			ParseStatus: types.ParseStatusPending, Metadata: types.JSON(metadata), CreatedAt: created, UpdatedAt: created}
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		if err := NewOperationLogService(tx).RecordTx(ctx, tx, "write_report", entry.ID, nil,
			map[string]any{"title": title, "vault_path": path, "related_article_ids": selected}); err != nil {
			return err
		}
		payload, _ := json.Marshal(types.ReportIndexPayload{SpaceID: kb.SpaceID, KnowledgeBaseID: kb.ID, EntryID: id})
		job, err := queue.NewClient(s.db).EnqueueTx(ctx, tx, queue.NewTask(types.TypeReportIndex, payload),
			queue.Queue(types.QueueMaintenance), queue.MaxRetry(5), queue.Timeout(30*time.Minute))
		if err == nil {
			jobID = job.ID
		}
		return err
	})
	if err != nil {
		if absolutePath != "" {
			if cleanupErr := os.Remove(absolutePath); cleanupErr != nil {
				return nil, errors.Join(err, fmt.Errorf("remove unsaved report: %w", cleanupErr))
			}
		}
		return nil, err
	}
	if !existing {
		s.observations[entry.VaultPath] = vaultObservation{hash: entry.FileHash, changedAt: entry.CreatedAt.Add(-s.reportStableWait)}
		if indexed, indexErr := s.retryReportIndex(ctx, kb, entry); indexErr == nil {
			entry = indexed
			// A worker may already have claimed the durable job. It will observe
			// the completed hash after this method releases the report mutex.
			_ = s.db.WithContext(ctx).Where("id=? AND state IN ?", jobID, []string{"pending", "scheduled"}).Delete(&queue.Job{}).Error
		} else {
			entry.ParseStatus, entry.ErrorMessage = types.ParseStatusFailed, indexErr.Error()
		}
	}
	return s.reportWriteResult(ctx, entry, existing)
}

func (s *VaultDocumentService) retryReportIndex(ctx context.Context, kb *types.KnowledgeBase, entry *types.Entry) (*types.Entry, error) {
	path, err := vaultAbsolute(s.root, entry.VaultPath)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("Markdown must be valid UTF-8")
	}
	var indexed *types.Entry
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactional := &VaultDocumentService{db: tx, models: s.models,
			retrieval: retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(tx))}
		indexed, err = transactional.indexReport(ctx, kb, entry.VaultPath, string(body), entry.CreatedAt)
		return err
	})
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, kb.SpaceID).
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
	_, kb, err := s.scope(ctx)
	if err != nil {
		return err
	}
	_, err = s.retryReportIndex(ctx, kb, &entry)
	return err
}
