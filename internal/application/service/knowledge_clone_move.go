package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	redis "github.com/redis/go-redis/v9"
	"time"

	"github.com/hibiken/asynq"

	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

const (
	knowledgeMoveProgressKeyPrefix = "knowledge_move_progress:"
	knowledgeMoveProgressTTL       = 24 * time.Hour
)

func getKnowledgeMoveProgressKey(taskID string) string {
	return knowledgeMoveProgressKeyPrefix + taskID
}

func (s *knowledgeService) saveKnowledgeMoveProgress(ctx context.Context, progress *types.KnowledgeMoveProgress) error {
	key := getKnowledgeMoveProgressKey(progress.TaskID)
	data, err := json.Marshal(progress)
	if err != nil {
		return fmt.Errorf("failed to marshal move progress: %w", err)
	}
	return s.redisClient.Set(ctx, key, data, knowledgeMoveProgressTTL).Err()
}

// SaveKnowledgeMoveProgress saves the knowledge move progress to Redis (public method for handler use)
func (s *knowledgeService) SaveKnowledgeMoveProgress(ctx context.Context, progress *types.KnowledgeMoveProgress) error {
	return s.saveKnowledgeMoveProgress(ctx, progress)
}

// GetKnowledgeMoveProgress retrieves the progress of a knowledge move task
func (s *knowledgeService) GetKnowledgeMoveProgress(ctx context.Context, taskID string) (*types.KnowledgeMoveProgress, error) {
	key := getKnowledgeMoveProgressKey(taskID)
	data, err := s.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, werrors.NewNotFoundError("Knowledge move task not found")
		}
		return nil, fmt.Errorf("failed to get move progress from Redis: %w", err)
	}

	var progress types.KnowledgeMoveProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return nil, fmt.Errorf("failed to unmarshal move progress: %w", err)
	}
	return &progress, nil
}

// ProcessKnowledgeMove handles Asynq knowledge move tasks
func (s *knowledgeService) ProcessKnowledgeMove(ctx context.Context, t *asynq.Task) error {
	var payload types.KnowledgeMovePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal knowledge move payload: %w", err)
	}
	ctx = payload.Initiator.Apply(ctx)

	// Add tenant ID to context
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)

	// Get tenant info and add to context
	tenantInfo, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Errorf(ctx, "ProcessKnowledgeMove: failed to get tenant info: %v", err)
		return fmt.Errorf("failed to get tenant info: %w", err)
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenantInfo)

	// Check if this is the last retry
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	isLastRetry := retryCount >= maxRetry

	logger.Infof(ctx, "ProcessKnowledgeMove: task=%s, source=%s, target=%s, mode=%s, count=%d, retry=%d/%d",
		payload.TaskID, payload.SourceKBID, payload.TargetKBID, payload.Mode, len(payload.KnowledgeIDs), retryCount, maxRetry)

	// Helper function to handle errors - only mark as failed on last retry
	handleError := func(progress *types.KnowledgeMoveProgress, err error, message string) {
		if isLastRetry {
			progress.Status = types.KnowledgeMoveStatusFailed
			progress.Error = err.Error()
			progress.Message = message
			progress.UpdatedAt = time.Now().Unix()
			_ = s.saveKnowledgeMoveProgress(ctx, progress)

		}
	}
	if retryCount == 0 {

	}

	// Update progress to processing
	progress := &types.KnowledgeMoveProgress{
		TaskID:     payload.TaskID,
		SourceKBID: payload.SourceKBID,
		TargetKBID: payload.TargetKBID,
		Status:     types.KnowledgeMoveStatusProcessing,
		Total:      len(payload.KnowledgeIDs),
		Progress:   0,
		Message:    "Starting knowledge move...",
		UpdatedAt:  time.Now().Unix(),
	}
	_ = s.saveKnowledgeMoveProgress(ctx, progress)

	// Get source and target knowledge bases
	sourceKB, err := s.kbService.GetKnowledgeBaseByID(ctx, payload.SourceKBID)
	if err != nil {
		handleError(progress, err, "Failed to get source knowledge base")
		return err
	}
	targetKB, err := s.kbService.GetKnowledgeBaseByID(ctx, payload.TargetKBID)
	if err != nil {
		handleError(progress, err, "Failed to get target knowledge base")
		return err
	}

	// Validate compatibility
	if sourceKB.Type != targetKB.Type {
		err := fmt.Errorf("type mismatch: source=%s, target=%s", sourceKB.Type, targetKB.Type)
		handleError(progress, err, "Source and target knowledge bases must be the same type")
		return err
	}
	if sourceKB.EmbeddingModelID != targetKB.EmbeddingModelID {
		err := fmt.Errorf("embedding model mismatch: source=%s, target=%s", sourceKB.EmbeddingModelID, targetKB.EmbeddingModelID)
		handleError(progress, err, "Source and target must use the same embedding model")
		return err
	}

	// Process each knowledge item
	for i, knowledgeID := range payload.KnowledgeIDs {
		err := s.moveOneKnowledge(ctx, knowledgeID, sourceKB, targetKB, payload.Mode)
		if err != nil {
			logger.Errorf(ctx, "ProcessKnowledgeMove: failed to move knowledge %s: %v", knowledgeID, err)
			progress.Failed++
		}
		progress.Processed = i + 1
		if progress.Total > 0 {
			progress.Progress = progress.Processed * 100 / progress.Total
		}
		progress.Message = fmt.Sprintf("Moved %d/%d knowledge items", progress.Processed, progress.Total)
		progress.UpdatedAt = time.Now().Unix()
		_ = s.saveKnowledgeMoveProgress(ctx, progress)
	}

	// Mark as completed
	if progress.Failed > 0 && progress.Failed == progress.Total {
		progress.Status = types.KnowledgeMoveStatusFailed
		progress.Message = fmt.Sprintf("Knowledge move failed: all %d items failed", progress.Total)
	} else {
		progress.Status = types.KnowledgeMoveStatusCompleted
		progress.Message = fmt.Sprintf("Knowledge move completed: %d/%d succeeded", progress.Processed-progress.Failed, progress.Total)
	}
	progress.Progress = 100
	progress.UpdatedAt = time.Now().Unix()
	_ = s.saveKnowledgeMoveProgress(ctx, progress)

	logger.Infof(ctx, "ProcessKnowledgeMove: task=%s completed, processed=%d, failed=%d", payload.TaskID, progress.Processed, progress.Failed)
	return nil
}

// moveOneKnowledge moves a single knowledge item from source KB to target KB.
func (s *knowledgeService) moveOneKnowledge(
	ctx context.Context,
	knowledgeID string,
	sourceKB, targetKB *types.KnowledgeBase,
	mode string,
) error {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// Get the knowledge item
	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		return fmt.Errorf("failed to get knowledge %s: %w", knowledgeID, err)
	}

	// Only move completed items
	if knowledge.ParseStatus != types.ParseStatusCompleted {
		return fmt.Errorf("knowledge %s is not in completed status (current: %s)", knowledgeID, knowledge.ParseStatus)
	}

	// Mark as processing during move
	knowledge.ParseStatus = types.ParseStatusProcessing
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		return fmt.Errorf("failed to mark knowledge as processing: %w", err)
	}

	switch mode {
	case "reuse_vectors":
		if err := s.moveKnowledgeReuseVectors(ctx, knowledge, sourceKB, targetKB); err != nil {
			return err
		}
		return nil
	case "reparse":
		return s.moveKnowledgeReparse(ctx, knowledge, sourceKB, targetKB)
	default:
		return fmt.Errorf("unknown move mode: %s", mode)
	}
}

// moveKnowledgeReuseVectors moves vector ownership and chunk references together.
func (s *knowledgeService) moveKnowledgeReuseVectors(
	ctx context.Context,
	knowledge *types.Knowledge,
	sourceKB, targetKB *types.KnowledgeBase,
) error {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// 1. Get old chunk IDs for vector index copy mapping
	var oldChunks []*types.Chunk
	if knowledge.EmbeddingModelID != "" {
		var err error
		oldChunks, err = s.chunkRepo.ListChunksByKnowledgeIDAndTypes(ctx, tenantID, knowledge.ID,
			[]types.ChunkType{types.ChunkTypeText, types.ChunkTypeParentText, types.ChunkTypeSummary,
				types.ChunkTypeImageOCR, types.ChunkTypeImageCaption, types.ChunkTypeTableSummary, types.ChunkTypeTableColumn})
		if err != nil {
			return fmt.Errorf("failed to list chunks: %w", err)
		}
	}

	// Build identity mapping (same chunk IDs, just moving between KBs)
	chunkIDMapping := make(map[string]string, len(oldChunks))
	for _, c := range oldChunks {
		chunkIDMapping[c.ID] = c.ID
	}

	// 2. Copy vector indices from source KB to target KB
	if len(chunkIDMapping) > 0 && knowledge.EmbeddingModelID != "" {
		retrieveEngine := s.retrieveEngine
		embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, knowledge.EmbeddingModelID)
		if err != nil {
			return fmt.Errorf("failed to get embedding model: %w", err)
		}

		// Identity mappings move the existing vectors to the destination KB.
		knowledgeIDMapping := map[string]string{knowledge.ID: knowledge.ID}
		if err := retrieveEngine.CopyIndices(ctx, sourceKB.ID, targetKB.ID,
			knowledgeIDMapping, chunkIDMapping,
			embeddingModel.GetDimensions(), sourceKB.Type,
		); err != nil {
			return fmt.Errorf("failed to copy indices: %w", err)
		}
	}

	// 3. Update chunks' knowledge_base_id in DB
	if err := s.chunkRepo.MoveChunksByKnowledgeID(ctx, tenantID, knowledge.ID, targetKB.ID); err != nil {
		return fmt.Errorf("failed to move chunks: %w", err)
	}

	// 4. Update the knowledge record.
	knowledge.KnowledgeBaseID = targetKB.ID
	knowledge.ParseStatus = types.ParseStatusCompleted
	knowledge.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		return fmt.Errorf("failed to update knowledge: %w", err)
	}

	return nil
}

// moveKnowledgeReparse moves knowledge to target KB and re-parses it with target KB's configuration.
func (s *knowledgeService) moveKnowledgeReparse(
	ctx context.Context,
	knowledge *types.Knowledge,
	_, targetKB *types.KnowledgeBase,
) error {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	// 1. Clean up existing chunks and vector indices
	if err := s.cleanupKnowledgeResources(ctx, knowledge); err != nil {
		logger.Warnf(ctx, "moveKnowledgeReparse: cleanup partial error for knowledge %s: %v", knowledge.ID, err)
		// Continue - partial cleanup is acceptable
	}

	// 2. Assign the knowledge to the target knowledge base.
	knowledge.KnowledgeBaseID = targetKB.ID
	knowledge.EmbeddingModelID = targetKB.EmbeddingModelID
	knowledge.ParseStatus = types.ParseStatusPending
	knowledge.EnableStatus = "disabled"
	knowledge.Description = ""
	knowledge.ProcessedAt = nil
	knowledge.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		return fmt.Errorf("failed to update knowledge: %w", err)
	}

	// 3. Enqueue document processing task with target KB's configuration
	if knowledge.IsManual() {
		meta, err := knowledge.ManualMetadata()
		if err != nil || meta == nil {
			return fmt.Errorf("failed to get manual metadata for reparse: %w", err)
		}
		s.triggerManualProcessing(ctx, targetKB, knowledge, meta.Content, false)
		return nil
	}

	if knowledge.FilePath != "" {
		enableMultimodel := targetKB.IsMultimodalEnabled()
		enableQuestionGeneration := false
		questionCount := 3
		if targetKB.QuestionGenerationConfig != nil && targetKB.QuestionGenerationConfig.Enabled {
			enableQuestionGeneration = true
			if targetKB.QuestionGenerationConfig.QuestionCount > 0 {
				questionCount = targetKB.QuestionGenerationConfig.QuestionCount
			}
		}

		lang := types.LanguageFromContextOrDefault(ctx)
		taskPayload := types.DocumentProcessPayload{
			TenantID:                 tenantID,
			KnowledgeID:              knowledge.ID,
			KnowledgeBaseID:          targetKB.ID,
			FilePath:                 knowledge.FilePath,
			FileName:                 knowledge.FileName,
			FileType:                 getFileType(knowledge.FileName),
			EnableMultimodel:         enableMultimodel,
			EnableQuestionGeneration: enableQuestionGeneration,
			QuestionCount:            questionCount,
			Language:                 lang,
		}

		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			return fmt.Errorf("failed to marshal document process payload: %w", err)
		}

		task := asynq.NewTask(types.TypeDocumentProcess, payloadBytes,
			documentProcessTaskOptions(s.config, asynq.MaxRetry(3))...)
		info, err := s.task.Enqueue(task)
		if err != nil {
			return fmt.Errorf("failed to enqueue document process task: %w", err)
		}
		logger.Infof(ctx, "moveKnowledgeReparse: enqueued reparse task id=%s for knowledge=%s", info.ID, knowledge.ID)
	}

	return nil
}
