package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type KnowledgeContextWorker struct {
	db        *gorm.DB
	context   *KnowledgeContextService
	knowledge interfaces.KnowledgeService
	chunks    interfaces.ChunkService
	tasks     interfaces.TaskEnqueuer
}

func NewKnowledgeContextWorker(db *gorm.DB, contextService *KnowledgeContextService, knowledge interfaces.KnowledgeService,
	chunks interfaces.ChunkService, tasks interfaces.TaskEnqueuer) *KnowledgeContextWorker {
	return &KnowledgeContextWorker{db: db, context: contextService, knowledge: knowledge, chunks: chunks, tasks: tasks}
}

func enqueueKnowledgeContext(ctx context.Context, tasks interfaces.TaskEnqueuer, payload types.KnowledgeAutoTagPayload) error {
	if tasks == nil {
		return errors.New("knowledge task queue unavailable")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tasks.Enqueue(asynq.NewTask(types.TypeKnowledgeContext, body), asynq.Queue(types.QueueGraph),
		asynq.MaxRetry(3), asynq.Timeout(10*time.Minute))
	return err
}

func (w *KnowledgeContextWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var p types.KnowledgeAutoTagPayload
	if err := json.Unmarshal(task.Payload(), &p); err != nil {
		return fmt.Errorf("context task: %w", err)
	}
	if p.TenantID == 0 || p.KnowledgeID == "" || p.KnowledgeBaseID == "" {
		return errors.New("invalid context task scope")
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, p.TenantID)
	k, err := w.knowledge.GetKnowledgeByID(ctx, p.KnowledgeID)
	if err != nil {
		return err
	}
	if k.KnowledgeBaseID != p.KnowledgeBaseID {
		return errors.New("context task knowledge base mismatch")
	}
	if k.ParseStatus == types.ParseStatusRejected || k.ParseStatus == types.ParseStatusFailed ||
		k.ParseStatus == types.ParseStatusDeleting || k.ParseStatus == types.ParseStatusCancelled || k.IsArchivedSource() {
		return nil
	}
	if k.IsSourceUpdateCandidate() {
		return nil
	}
	intake, err := k.IntakeMetadata()
	if err != nil {
		return err
	}
	if intake != nil && intake.Decision != "approved" {
		return nil
	}
	if err := updateKnowledgeContextState(ctx, w.db, k, "processing", ""); err != nil {
		return err
	}
	body, err := w.messageBody(ctx, k)
	if err == nil {
		err = w.context.ProcessMessage(ctx, k, body)
	}
	if err != nil {
		_ = updateKnowledgeContextState(ctx, w.db, k, "failed", err.Error())
		return err
	}
	return updateKnowledgeContextState(ctx, w.db, k, "completed", "")
}

// Rebuild applies the same intake and scope guards as ordinary enrichment.
// It queues existing messages; it does not pretend historical data is already migrated.
func (w *KnowledgeContextWorker) Rebuild(ctx context.Context, kbID string, ids []string) (int, error) {
	items, err := w.knowledge.ListKnowledgeByKnowledgeBaseID(ctx, kbID)
	if err != nil {
		return 0, err
	}
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	count := 0
	for _, k := range items {
		if len(selected) > 0 && !selected[k.ID] {
			continue
		}
		if k.IsArchivedSource() || k.IsSourceUpdateCandidate() || (k.ParseStatus != types.ParseStatusCompleted && k.ParseStatus != types.ParseStatusFinalizing) {
			continue
		}
		intake, err := k.IntakeMetadata()
		if err != nil {
			return count, err
		}
		if intake != nil && intake.Decision != "approved" {
			continue
		}
		if err := enqueueKnowledgeContext(ctx, w.tasks, types.KnowledgeAutoTagPayload{TenantID: k.TenantID, KnowledgeBaseID: kbID, KnowledgeID: k.ID}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (w *KnowledgeContextWorker) messageBody(ctx context.Context, k *types.Knowledge) (string, error) {
	if concrete, ok := w.knowledge.(*knowledgeService); ok {
		return concrete.contextMessageBody(ctx, k)
	}
	return contextBodyFromChunks(ctx, w.chunks, k.ID)
}

func (s *knowledgeService) contextMessageBody(ctx context.Context, k *types.Knowledge) (string, error) {
	intake, err := k.IntakeMetadata()
	if err != nil {
		return "", err
	}
	if intake != nil && intake.OriginalPath != "" {
		reader, err := s.resolveFileServiceForPath(ctx, nil, intake.OriginalPath).GetFile(ctx, intake.OriginalPath)
		if err != nil {
			return "", err
		}
		defer reader.Close()
		const maxEvidenceBodyBytes = 32 << 20
		body, err := io.ReadAll(io.LimitReader(reader, maxEvidenceBodyBytes+1))
		if err != nil {
			return "", err
		}
		if len(body) > maxEvidenceBodyBytes {
			return "", errors.New("source too large for context evidence; original remains available")
		}
		return string(body), nil
	}
	if k.IsManual() {
		meta, err := k.ManualMetadata()
		if err != nil {
			return "", err
		}
		if meta != nil && strings.TrimSpace(meta.Content) != "" {
			return meta.Content, nil
		}
	}
	return contextBodyFromChunks(ctx, s.chunkService, k.ID)
}

func contextBodyFromChunks(ctx context.Context, chunks interfaces.ChunkService, knowledgeID string) (string, error) {
	if chunks == nil {
		return "", errors.New("source body unavailable")
	}
	items, err := chunks.ListChunksByKnowledgeID(ctx, knowledgeID)
	if err != nil {
		return "", err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ChunkIndex < items[j].ChunkIndex })
	var body strings.Builder
	for _, chunk := range items {
		if chunk.ChunkType != types.ChunkTypeText {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(chunk.Content)
	}
	if strings.TrimSpace(body.String()) == "" {
		return "", errors.New("no source text available for context")
	}
	return body.String(), nil
}

func updateKnowledgeContextState(ctx context.Context, db *gorm.DB, k *types.Knowledge, status, detail string) error {
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "metadata").Where("id = ? AND tenant_id = ?", k.ID, k.TenantID).First(&current).Error; err != nil {
			return err
		}
		var meta map[string]json.RawMessage
		if len(current.Metadata) > 0 {
			if err := json.Unmarshal(current.Metadata, &meta); err != nil {
				return err
			}
		}
		if meta == nil {
			meta = make(map[string]json.RawMessage)
		}
		detailRunes := []rune(detail)
		if len(detailRunes) > 500 {
			detail = string(detailRunes[:500])
		}
		meta["knowledge_context"], _ = json.Marshal(map[string]any{"status": status, "error": detail, "updated_at": time.Now().UTC()})
		encoded, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		return tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ?", k.ID, k.TenantID).Update("metadata", types.JSON(encoded)).Error
	})
}

func (s *knowledgeService) snapshotContextSource(ctx context.Context, k *types.Knowledge) error {
	if s.db == nil || !s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		return nil
	}
	body, err := s.contextMessageBody(ctx, k)
	if err != nil {
		return err
	}
	_, err = NewKnowledgeContextService(s.db, s.modelService).SnapshotMessage(ctx, k, body)
	return err
}

func (s *knowledgeService) queueContextSource(ctx context.Context, k *types.Knowledge) {
	if s.db == nil || !s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		return
	}
	if err := enqueueKnowledgeContext(ctx, s.task, types.KnowledgeAutoTagPayload{TenantID: k.TenantID, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID}); err != nil {
		logger.Warnf(ctx, "Failed to queue structured context for %s: %v", k.ID, err)
		_ = updateKnowledgeContextState(ctx, s.db, k, "failed", err.Error())
	}
}

// evidenceImageReferences is used before resource teardown so archived images
// are retained by edits and reclaimed by explicit deletion with their source.
func evidenceImageReferences(ctx context.Context, db *gorm.DB, tenantID uint64, knowledgeIDs []string) ([]string, error) {
	if db == nil || !db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		return nil, nil
	}
	var evidence []types.KnowledgeEvidence
	if err := db.WithContext(ctx).Select("image_refs").Where("tenant_id = ? AND knowledge_id IN ?", tenantID, knowledgeIDs).Find(&evidence).Error; err != nil {
		return nil, err
	}
	var refs []string
	seen := map[string]bool{}
	for _, item := range evidence {
		for _, ref := range item.ImageRefs {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs, nil
}
