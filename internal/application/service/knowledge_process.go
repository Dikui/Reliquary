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

	"github.com/google/uuid"
	taskqueue "reliquary/internal/queue"

	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"
	webfetch "reliquary/internal/infrastructure/webfetch"
	"reliquary/internal/logger"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// processDocumentFromPassage handles asynchronous processing of text passages
func (s *knowledgeService) processDocumentFromPassage(ctx context.Context,
	kb *types.KnowledgeBase, knowledge *types.Knowledge, passage []string,
) {
	// Update status to processing
	knowledge.ParseStatus = "processing"
	knowledge.UpdatedAt = time.Now()
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		return
	}

	if err := s.processArticleBody(ctx, kb, knowledge, strings.Join(passage, "\n\n"), nil, types.EffectiveProcessConfig{ChunkingConfig: kb.ChunkingConfig}); err != nil {
		s.markArticleProcessingFailed(ctx, knowledge, err)
	}
}

// ProcessChunksOptions contains options for processing chunks
type ProcessChunksOptions struct {
	// ParentChunks holds parent chunk data when parent-child chunking is enabled.
	// When set, the chunks passed to processChunks are child chunks, and each
	// child's ParentIndex references an entry in this slice.
	ParentChunks []types.ParsedParentChunk
}

// finalizeIndexedKnowledgeState records the completed synchronous processing attempt.
func finalizeIndexedKnowledgeState(knowledge *types.Knowledge, totalStorageSize int64, now time.Time) {
	knowledge.ParseStatus = types.ParseStatusCompleted
	knowledge.SummaryStatus = types.SummaryStatusCompleted
	knowledge.ErrorMessage = ""
	knowledge.EnableStatus = "enabled"
	knowledge.StorageSize = totalStorageSize
	knowledge.ProcessedAt = &now
	knowledge.UpdatedAt = now
}

// markKnowledgeProcessing makes the top-level knowledge state describe the
// attempt that is starting now. Clearing error_message is part of that: the
// column is only meaningful for the attempt that produced it, so a row
// re-entering processing must not keep surfacing the previous attempt's
// failure while it is visibly running again.
func markKnowledgeProcessing(knowledge *types.Knowledge, now time.Time) {
	knowledge.ParseStatus = types.ParseStatusProcessing
	knowledge.ErrorMessage = ""
	knowledge.UpdatedAt = now
}

// buildSplitterConfig creates a SplitterConfig with fallbacks from a KnowledgeBase.
// Defaults mirror chunker.DefaultChunkSize / DefaultChunkOverlap so behavior is
// identical whether callers come through this path or invoke the chunker
// directly with a zero-value config.
func buildSplitterConfig(kb *types.KnowledgeBase) chunker.SplitterConfig {
	return buildSplitterConfigFromChunking(kb.ChunkingConfig)
}

func buildSplitterConfigFromChunking(cc types.ChunkingConfig) chunker.SplitterConfig {
	return chunker.NormalizeSplitterConfig(chunker.SplitterConfig{
		ChunkSize:       cc.ChunkSize,
		ChunkOverlap:    cc.ChunkOverlap,
		ChunkOverlapSet: cc.ChunkOverlapSet,
		Separators:      cc.Separators,
		Strategy:        cc.Strategy,
		TokenLimit:      cc.TokenLimit,
		Languages:       cc.Languages,
	})
}

// buildParentChildConfigs derives parent and child SplitterConfig from ChunkingConfig.
// The base config (already validated with defaults) is used for separators and
// the splitting strategy. Strategy must be propagated: an empty Strategy resolves
// to the legacy tier (see resolveChainWithProfile), which never runs the heading
// splitter, so parent-child chunks would silently lose heading alignment and
// ContextHeader breadcrumbs regardless of the configured strategy.
func buildParentChildConfigs(cc types.ChunkingConfig, base chunker.SplitterConfig) (parent, child chunker.SplitterConfig) {
	return chunker.DeriveParentChildConfigs(base, cc.ParentChunkSize, cc.ChildChunkSize)
}

// processChunks processes chunks and creates embeddings for knowledge content
func (s *knowledgeService) processChunks(ctx context.Context,
	kb *types.KnowledgeBase, knowledge *types.Knowledge, chunks []types.ParsedChunk,
	opts ...ProcessChunksOptions,
) {
	ctx, _ = types.WithModelCallCollector(ctx)
	ctx = types.WithLLMCallMetadata(ctx, "document_embedding", "")
	// Get options
	var options ProcessChunksOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	// Parser output and manually supplied passages can contain malformed byte
	// sequences. Clean them before logging, chunk persistence, or embedding;
	// the embedding provider and tracing/database drivers expect valid UTF-8.
	for i := range chunks {
		chunks[i].Content = secutils.CleanInvalidUTF8(chunks[i].Content)
		chunks[i].ContextHeader = secutils.CleanInvalidUTF8(chunks[i].ContextHeader)
		for j := range chunks[i].Images {
			chunks[i].Images[j].URL = secutils.CleanInvalidUTF8(chunks[i].Images[j].URL)
			chunks[i].Images[j].Caption = secutils.CleanInvalidUTF8(chunks[i].Images[j].Caption)
			chunks[i].Images[j].OCRText = secutils.CleanInvalidUTF8(chunks[i].Images[j].OCRText)
			chunks[i].Images[j].OriginalURL = secutils.CleanInvalidUTF8(chunks[i].Images[j].OriginalURL)
		}
	}
	for i := range options.ParentChunks {
		options.ParentChunks[i].Content = secutils.CleanInvalidUTF8(options.ParentChunks[i].Content)
	}

	// Check if knowledge is being deleted/cancelled before processing.
	// Both statuses short-circuit identically here — there's nothing to clean
	// up yet so the branch is purely "stop early".
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		logger.Infof(ctx, "Knowledge aborted (%s), skipping chunk processing: %s", status, knowledge.ID)
		return
	}

	// Get embedding model for vectorization — only needed when vector/keyword indexing is enabled
	var embeddingModel embedding.Embedder
	if kb.NeedsEmbeddingModel() {
		var err error
		embeddingModel, err = GetTierEmbeddingModel(ctx, s.db, s.modelService, kb)
		if err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("processChunks get embedding model failed")
			knowledge.ParseStatus, knowledge.ErrorMessage = types.ParseStatusFailed, err.Error()
			_ = s.repo.UpdateKnowledge(ctx, knowledge)
			return
		}
		knowledge.EmbeddingModelID = embeddingModel.GetModelID()
	} else {
		logger.Infof(ctx, "Vector/keyword indexing disabled for KB %s, skipping embedding model", kb.ID)
	}

	// 幂等性处理：清理旧的chunks和索引数据，避免重复数据
	logger.Infof(ctx, "Cleaning up existing chunks and index data for knowledge: %s", knowledge.ID)

	// 删除旧的chunks
	if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
		logger.Warnf(ctx, "Failed to delete existing chunks (may not exist): %v", err)
		// 不返回错误，继续处理（可能没有旧数据）
	}

	// 删除旧的索引数据 — only when vector/keyword indexing is enabled
	tenantInfo := ctx.Value(types.TenantInfoContextKey).(*types.Tenant)
	var err error
	retrieveEngine := s.retrieveEngine
	if embeddingModel != nil {
		if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, embeddingModel.GetDimensions(), knowledge.Type); err != nil {
			logger.Warnf(ctx, "Failed to delete existing index data (may not exist): %v", err)
			// 不返回错误，继续处理（可能没有旧数据）
		} else {
			logger.Infof(ctx, "Successfully deleted existing index data for knowledge: %s", knowledge.ID)
		}
	}

	logger.Infof(ctx, "Cleanup completed, starting to process new chunks")

	// ========== DocReader 解析结果日志 ==========
	logger.Infof(ctx, "[DocReader] ========== 解析结果概览 ==========")
	logger.Infof(ctx, "[DocReader] 知识ID: %s, 知识库ID: %s", knowledge.ID, knowledge.KnowledgeBaseID)
	logger.Infof(ctx, "[DocReader] 总Chunk数量: %d", len(chunks))

	// 统计图片信息
	totalImages := 0
	chunksWithImages := 0
	for _, chunkData := range chunks {
		if len(chunkData.Images) > 0 {
			chunksWithImages++
			totalImages += len(chunkData.Images)
		}
	}
	logger.Infof(ctx, "[DocReader] 包含图片的Chunk数: %d, 总图片数: %d", chunksWithImages, totalImages)

	// 打印每个Chunk的详细信息
	for idx, chunkData := range chunks {
		contentPreview := chunkData.Content
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200] + "..."
		}
		logger.Infof(ctx, "[DocReader] Chunk #%d (seq=%d): 内容长度=%d, 图片数=%d, 范围=[%d-%d]",
			idx, chunkData.Seq, len(chunkData.Content), len(chunkData.Images), chunkData.Start, chunkData.End)
		logger.Debugf(ctx, "[DocReader] Chunk #%d 内容预览: %s", idx, contentPreview)

		// 打印图片详细信息
		for imgIdx, img := range chunkData.Images {
			logger.Infof(ctx, "[DocReader]   图片 #%d: URL=%s", imgIdx, img.URL)
			logger.Infof(ctx, "[DocReader]   图片 #%d: OriginalURL=%s", imgIdx, img.OriginalURL)
			if img.Caption != "" {
				captionPreview := img.Caption
				if len(captionPreview) > 100 {
					captionPreview = captionPreview[:100] + "..."
				}
				logger.Infof(ctx, "[DocReader]   图片 #%d: Caption=%s", imgIdx, captionPreview)
			}
			if img.OCRText != "" {
				ocrPreview := img.OCRText
				if len(ocrPreview) > 100 {
					ocrPreview = ocrPreview[:100] + "..."
				}
				logger.Infof(ctx, "[DocReader]   图片 #%d: OCRText=%s", imgIdx, ocrPreview)
			}
			logger.Infof(ctx, "[DocReader]   图片 #%d: 位置=[%d-%d]", imgIdx, img.Start, img.End)
		}
	}
	logger.Infof(ctx, "[DocReader] ========== 解析结果概览结束 ==========")

	// === Parent-Child Chunking: create parent chunks first ===
	hasParentChild := len(options.ParentChunks) > 0
	var parentDBChunks []*types.Chunk // indexed by ParsedParentChunk position
	if hasParentChild {
		parentDBChunks = make([]*types.Chunk, len(options.ParentChunks))
		for i, pc := range options.ParentChunks {
			parentDBChunks[i] = &types.Chunk{
				ID:              uuid.New().String(),
				TenantID:        knowledge.TenantID,
				KnowledgeID:     knowledge.ID,
				KnowledgeBaseID: knowledge.KnowledgeBaseID,
				Content:         pc.Content,
				ChunkIndex:      pc.Seq,
				IsEnabled:       true,
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
				StartAt:         pc.Start,
				EndAt:           pc.End,
				ChunkType:       types.ChunkTypeParentText,
			}
		}
		// Set prev/next links for parent chunks
		for i := range parentDBChunks {
			if i > 0 {
				parentDBChunks[i-1].NextChunkID = parentDBChunks[i].ID
				parentDBChunks[i].PreChunkID = parentDBChunks[i-1].ID
			}
		}
		logger.Infof(ctx, "Created %d parent chunks for parent-child strategy", len(parentDBChunks))
	}

	// Reserve text chunks and optional parent chunks.
	parentCount := len(options.ParentChunks)
	insertChunks := make([]*types.Chunk, 0, len(chunks)+parentCount)
	// Add parent chunks first (they go into DB but NOT into the vector index)
	if hasParentChild {
		insertChunks = append(insertChunks, parentDBChunks...)
	}

	for idx, chunkData := range chunks {
		if strings.TrimSpace(chunkData.Content) == "" {
			continue
		}

		// 创建主文本Chunk
		textChunk := &types.Chunk{
			ID:              uuid.New().String(),
			TenantID:        knowledge.TenantID,
			KnowledgeID:     knowledge.ID,
			KnowledgeBaseID: knowledge.KnowledgeBaseID,
			Content:         chunkData.Content,
			ContextHeader:   chunkData.ContextHeader,
			ChunkIndex:      int(chunkData.Seq),
			IsEnabled:       true,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
			StartAt:         int(chunkData.Start),
			EndAt:           int(chunkData.End),
			ChunkType:       types.ChunkTypeText,
		}
		if len(chunkData.Images) > 0 {
			infos := make([]types.ImageInfo, 0, len(chunkData.Images))
			for _, image := range chunkData.Images {
				infos = append(infos, types.ImageInfo{URL: image.URL, OriginalURL: image.OriginalURL, StartPos: image.Start, EndPos: image.End})
			}
			encoded, _ := json.Marshal(infos)
			textChunk.ImageInfo = string(encoded)
		}

		// Wire up ParentChunkID for child chunks
		if hasParentChild && chunkData.ParentIndex >= 0 && chunkData.ParentIndex < len(parentDBChunks) {
			textChunk.ParentChunkID = parentDBChunks[chunkData.ParentIndex].ID
		}

		chunks[idx].ChunkID = textChunk.ID
		insertChunks = append(insertChunks, textChunk)
	}

	// Sort chunks by index for proper ordering
	sort.Slice(insertChunks, func(i, j int) bool {
		return insertChunks[i].ChunkIndex < insertChunks[j].ChunkIndex
	})

	// Collect retrievable text chunks only. ParentChunkID only controls parent expansion after retrieval.
	// When ParentChunkID is empty, retrieval keeps the standalone child content without loading a parent.
	textChunks := make([]*types.Chunk, 0, len(chunks))
	for _, chunk := range insertChunks {
		if chunk.ChunkType == types.ChunkTypeText {
			textChunks = append(textChunks, chunk)
		}
	}

	// 设置文本Chunk之间的前后关系 (skip if parent-child, children don't need prev/next links)
	if !hasParentChild {
		for i, chunk := range textChunks {
			if i > 0 {
				textChunks[i-1].NextChunkID = chunk.ID
			}
			if i < len(textChunks)-1 {
				textChunks[i+1].PreChunkID = chunk.ID
			}
		}
	}

	// Check if knowledge is being deleted/cancelled before writing chunks.
	// Nothing has been persisted yet, so both branches just bail.
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		logger.Infof(ctx, "Knowledge aborted (%s), skipping chunk write: %s", status, knowledge.ID)
		return
	}

	// Save chunks to database — ALWAYS, regardless of indexing strategy.
	// Chunks are needed for knowledge context and summary generation
	// even when vector/keyword indexing is disabled.
	s.beginStage(ctx, knowledge.ID, types.StageChunking, types.JSONMap{
		"chunks_planned": len(insertChunks),
	})
	if err := s.chunkService.CreateChunks(ctx, insertChunks); err != nil {
		knowledge.ParseStatus = types.ParseStatusFailed
		knowledge.ErrorMessage = err.Error()
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		s.failStage(ctx, knowledge.ID, types.StageChunking,
			werrors.ErrCodeChunkingFailed, "create chunks failed", err)
		return
	}
	totalChunkChars := 0
	for _, c := range insertChunks {
		totalChunkChars += len(c.Content)
	}
	s.endStage(ctx, knowledge.ID, types.StageChunking, types.JSONMap{
		"chunks_written":   len(insertChunks),
		"total_text_chars": totalChunkChars,
	})

	// Create index information and perform vector indexing, only when vector/keyword is enabled.
	// Chunks are ALWAYS saved to DB (above) because knowledge context needs them even without vector indexing.
	var totalStorageSize int64
	if kb.NeedsEmbeddingModel() && embeddingModel != nil {
		embedInput := types.JSONMap{
			"chunks_to_embed": len(textChunks),
			"model_id":        kb.EmbeddingModelID,
		}
		if dim := embeddingModel.GetDimensions(); dim > 0 {
			embedInput["dim"] = dim
		}
		s.beginStage(ctx, knowledge.ID, types.StageEmbedding, embedInput)
		// Create index information, only for child/flat chunks, NOT parent chunks.
		// Parent chunks are stored for context retrieval but do not need vector embeddings.
		// Supply the article source prefix only to the vector input.
		indexInfoList := make([]*types.IndexInfo, 0, len(textChunks))
		for _, chunk := range textChunks {
			// Heading breadcrumbs remain stored separately. Vector inputs use
			// exactly one article prefix line followed by the chunk body.
			indexContent := buildKnowledgeIndexContent(knowledge, chunk.Content)
			indexInfoList = append(indexInfoList, &types.IndexInfo{
				Content:         indexContent,
				SourceID:        chunk.ID,
				SourceType:      types.ChunkSourceType,
				ChunkID:         chunk.ID,
				KnowledgeID:     knowledge.ID,
				KnowledgeBaseID: knowledge.KnowledgeBaseID,
				IsEnabled:       true,
			})
		}

		// Calculate storage size required for embeddings
		totalStorageSize = retrieveEngine.EstimateStorageSize(ctx, embeddingModel, indexInfoList)

		// Check again before batch indexing (heavy operation).
		// deleting → row is going away anyway, drop the chunks we just wrote.
		// cancelled → user wants to keep what was already persisted, just stop.
		if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
			logger.Infof(ctx, "Knowledge aborted (%s) before indexing: %s", status, knowledge.ID)
			if status == types.ParseStatusDeleting {
				if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
					logger.Warnf(ctx, "Failed to cleanup chunks after deletion detected: %v", err)
				}
			}
			return
		}

		err = retrieveEngine.BatchIndex(ctx, embeddingModel, indexInfoList)
		if err != nil {
			knowledge.ParseStatus = types.ParseStatusFailed
			knowledge.ErrorMessage = err.Error()
			knowledge.UpdatedAt = time.Now()
			s.repo.UpdateKnowledge(ctx, knowledge)

			// delete failed chunks
			if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
				logger.Errorf(ctx, "Delete chunks failed: %v", err)
			}

			// delete index
			if err := retrieveEngine.DeleteByKnowledgeIDList(
				ctx, []string{knowledge.ID}, embeddingModel.GetDimensions(), kb.Type,
			); err != nil {
				logger.Errorf(ctx, "Delete index failed: %v", err)
			}
			// Map PostgreSQL index / embedding rate-limit errors to a
			// stable code so the UI can offer "retry later" hints.
			code := werrors.ErrCodeIndexWriteFailed
			if isLikelyRateLimitError(err) {
				code = werrors.ErrCodeEmbeddingRateLimit
			}
			s.failStage(ctx, knowledge.ID, types.StageEmbedding,
				code, "batch index failed", err)
			return
		}
		logger.GetLogger(ctx).Infof("processChunks batch index successfully, with %d index", len(indexInfoList))
		s.endStage(ctx, knowledge.ID, types.StageEmbedding, types.JSONMap{
			"vectors_written": len(indexInfoList),
			"storage_bytes":   totalStorageSize,
		})

		// Final check before marking as completed.
		// deleting → drop chunks+index we just wrote.
		// cancelled → keep persisted data; the row stays in cancelled status
		// and downstream stages skip via the entry guards.
		if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
			logger.Infof(ctx, "Knowledge aborted (%s) after indexing: %s", status, knowledge.ID)
			if status == types.ParseStatusDeleting {
				if err := s.chunkService.DeleteChunksByKnowledgeID(ctx, knowledge.ID); err != nil {
					logger.Warnf(ctx, "Failed to cleanup chunks after deletion detected: %v", err)
				}
				if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, []string{knowledge.ID}, embeddingModel.GetDimensions(), kb.Type); err != nil {
					logger.Warnf(ctx, "Failed to cleanup index after deletion detected: %v", err)
				}
			}
			return
		}
	} else {
		logger.Infof(ctx, "Vector/keyword indexing disabled for KB %s, skipping BatchIndex", kb.ID)
		s.skipStage(ctx, knowledge.ID, types.StageEmbedding, "skipped")
	}

	now := time.Now()
	finalizeIndexedKnowledgeState(knowledge, totalStorageSize, now)
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		knowledge.ParseStatus = types.ParseStatusFailed
		knowledge.ErrorMessage = err.Error()
	}

	// Update tenant's storage usage
	tenantInfo.StorageUsed += totalStorageSize
	if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantInfo.ID, totalStorageSize); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("processChunks update tenant storage used failed")
	}
	logger.GetLogger(ctx).Infof("processChunks successfully")
}

func (s *knowledgeService) ReparseKnowledge(
	ctx context.Context,
	knowledgeID string,
	processOverrides *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.ReparseKnowledge(ctx, knowledgeID, processOverrides)
		})
	}

	logger.Info(ctx, "Start re-parsing knowledge")

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	existing, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		logger.Errorf(ctx, "Failed to load knowledge: %v", err)
		return nil, err
	}
	if existing.SourceKind != "" && existing.SourceKind != "article" {
		return nil, werrors.NewBadRequestError("vault documents are indexed directly from their files")
	}

	if intake, _ := existing.IntakeMetadata(); intake != nil && (intake.Decision == "rejected" || intake.Decision == "error") {
		return nil, werrors.NewBadRequestError("use the intake review action for this message")
	}

	// Allocate a fresh span tree attempt up front. Doing this BEFORE
	// the cleanup + enqueue means: (a) the UI immediately sees a new
	// attempt with all five stages back to "pending" instead of the
	// previous run's "failed" badge lingering; (b) the worker's
	// fallback path won't double-allocate when payload.Attempt is
	// already set on the queued task.
	reparseAttempt := 0
	if root, n, err := s.tracker().OpenAttempt(ctx, existing.ID); err == nil && root != nil {
		reparseAttempt = n
	} else if err != nil {
		logger.Warnf(ctx, "[Reparse] OpenAttempt failed for %s: %v (will fall back in worker)", existing.ID, err)
	}

	// Get knowledge base configuration
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, existing.KnowledgeBaseID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base for reparse: %v", err)
		return nil, err
	}

	// When the caller supplies new overrides (e.g. via the reparse confirm
	// dialog), validate them against this knowledge's file type, then persist
	// to metadata so both this call's enqueue and the worker re-read the same
	// config. nil keeps whatever was stored at upload time.
	if processOverrides != nil {
		if err := ValidateProcessOverrides(ctx, kb, processOverrides, reparseFileTypes(existing)); err != nil {
			return nil, err
		}
		if err := existing.SetProcessOverrides(processOverrides); err != nil {
			logger.Errorf(ctx, "Failed to set process overrides on reparse: %v", err)
			return nil, err
		}
		if err := s.repo.UpdateKnowledgeColumn(ctx, existing.ID, "metadata", existing.Metadata); err != nil {
			logger.Errorf(ctx, "Failed to persist process overrides on reparse: %v", err)
			return nil, err
		}
	}

	recordReparseStarted := func() {
	}

	// A summary-only file import may have its original source removed before a
	// user later converts it to full storage. Its first vault body is then the
	// authoritative source for reprocessing, even though its import type is file.
	if existing.ProcessingMode == types.ProcessingModeFull && s.vault != nil && existing.VaultPath != "" && existing.FilePath == "" && !existing.IsManual() {
		body, err := s.vault.ReadBody(existing)
		if err != nil {
			return nil, err
		}
		resetKnowledgeForReparse(existing, kb)
		if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
			return nil, err
		}
		if _, err := s.enqueueManualProcessing(ctx, existing, string(body), true); err != nil {
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, err
		}
		return existing, nil
	}

	if existing.ProcessingMode == "summary_only" {
		source, err := articleSourceInfo(existing)
		if err != nil {
			return nil, err
		}
		url := source.OriginalURL
		if url == "" && (existing.Type == "url" || existing.Type == "file_url") {
			url = existing.Source
		}
		if url == "" {
			err := errors.New("cannot reprocess summary-only article: original URL is missing")
			s.markArticleProcessingFailed(ctx, existing, err)
			return existing, werrors.NewBadRequestError(err.Error())
		}
		resetKnowledgeForReparse(existing, kb)
		if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
			return nil, err
		}
		payload := types.DocumentProcessPayload{TenantID: tenantID, KnowledgeID: existing.ID, KnowledgeBaseID: existing.KnowledgeBaseID, URL: url, Language: types.LanguageFromContextOrDefault(ctx), Attempt: reparseAttempt}
		if existing.Type == "file_url" || (existing.Type == "file" && isFileURL(url, "", "")) {
			payload.URL = ""
			payload.FileURL = url
			payload.FileName = existing.FileName
			payload.FileType = existing.FileType
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if _, err = s.task.Enqueue(taskqueue.NewTask(types.TypeDocumentProcess, encoded, documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...)); err != nil {
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, err
		}
		return existing, nil
	}

	// For manual knowledge, use async manual processing (cleanup + re-indexing in worker)
	if existing.IsManual() {
		meta, metaErr := existing.ManualMetadata()
		if metaErr != nil || meta == nil {
			logger.Errorf(ctx, "Failed to get manual metadata for reparse: %v", metaErr)
			return nil, werrors.NewBadRequestError("无法获取手工知识内容")
		}

		if intake, _ := existing.IntakeMetadata(); intake != nil || (s.vault != nil && existing.VaultPath != "") {
			file, _, err := s.GetKnowledgeBody(ctx, existing.ID)
			if err != nil {
				return nil, err
			}
			body, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				return nil, err
			}
			meta.Content = string(body)
		}

		resetKnowledgeForReparse(existing, kb)

		if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
			logger.Errorf(ctx, "Failed to update knowledge status before reparse: %v", err)
			return nil, err
		}
		if err := s.repo.UpdateKnowledgeColumn(ctx, existing.ID, "pending_subtasks_count", 0); err != nil {
			logger.Errorf(ctx, "Failed to reset pending_subtasks_count before reparse: %v", err)
			return nil, err
		}

		if _, err := s.enqueueManualProcessing(ctx, existing, meta.Content, true); err != nil {
			logger.Errorf(ctx, "Failed to enqueue manual reparse task: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		} else {
			recordReparseStarted()
		}
		return existing, nil
	}

	// For non-manual knowledge, cleanup synchronously then enqueue document processing
	logger.Infof(ctx, "Cleaning up existing resources for knowledge: %s", knowledgeID)
	if err := s.cleanupKnowledgeResources(ctx, existing); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": knowledgeID,
		})
		return nil, err
	}

	// Step 2: Update knowledge status and metadata
	resetKnowledgeForReparse(existing, kb)

	if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
		logger.Errorf(ctx, "Failed to update knowledge status before reparse: %v", err)
		return nil, err
	}
	if err := s.repo.UpdateKnowledgeColumn(ctx, existing.ID, "pending_subtasks_count", 0); err != nil {
		logger.Errorf(ctx, "Failed to reset pending_subtasks_count before reparse: %v", err)
		return nil, err
	}

	// Step 3: Trigger async re-parsing based on knowledge type
	logger.Infof(ctx, "Knowledge status updated, scheduling async reparse, ID: %s, Type: %s", existing.ID, existing.Type)

	// For file-based knowledge, enqueue document processing task
	if existing.FilePath != "" {
		tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

		lang := types.LanguageFromContextOrDefault(ctx)
		taskPayload := types.DocumentProcessPayload{
			TenantID:        tenantID,
			KnowledgeID:     existing.ID,
			KnowledgeBaseID: existing.KnowledgeBaseID,
			FilePath:        existing.FilePath,
			FileName:        existing.FileName,
			FileType:        getFileType(existing.FileName),
			Language:        lang,
			Attempt:         reparseAttempt,
		}

		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorf(ctx, "Failed to marshal reparse task payload: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}

		task := taskqueue.NewTask(
			types.TypeDocumentProcess,
			payloadBytes,
			documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
		)
		info, err := s.task.Enqueue(task)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue reparse task: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}
		logger.Infof(ctx, "Enqueued reparse task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, existing.ID)
		recordReparseStarted()

		return existing, nil
	}

	// For file-URL-based knowledge, enqueue document processing task with FileURL field
	if existing.Type == "file_url" && existing.Source != "" {
		tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

		lang := types.LanguageFromContextOrDefault(ctx)
		taskPayload := types.DocumentProcessPayload{
			TenantID:        tenantID,
			KnowledgeID:     existing.ID,
			KnowledgeBaseID: existing.KnowledgeBaseID,
			FileURL:         existing.Source,
			FileName:        existing.FileName,
			FileType:        existing.FileType,
			Language:        lang,
			Attempt:         reparseAttempt,
		}

		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorf(ctx, "Failed to marshal file URL reparse task payload: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}

		task := taskqueue.NewTask(
			types.TypeDocumentProcess,
			payloadBytes,
			documentProcessTaskOptions(s.config)...,
		)
		info, err := s.task.Enqueue(task)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue file URL reparse task: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}
		logger.Infof(ctx, "Enqueued file URL reparse task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, existing.ID)
		recordReparseStarted()

		return existing, nil
	}

	// For URL-based knowledge, enqueue URL processing task
	if existing.Type == "url" && existing.Source != "" {
		tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

		lang := types.LanguageFromContextOrDefault(ctx)
		taskPayload := types.DocumentProcessPayload{
			TenantID:        tenantID,
			KnowledgeID:     existing.ID,
			KnowledgeBaseID: existing.KnowledgeBaseID,
			URL:             existing.Source,
			Language:        lang,
			Attempt:         reparseAttempt,
		}

		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorf(ctx, "Failed to marshal URL reparse task payload: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}

		task := taskqueue.NewTask(
			types.TypeDocumentProcess,
			payloadBytes,
			documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
		)
		info, err := s.task.Enqueue(task)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue URL reparse task: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, existing)
			return existing, werrors.NewInternalServerError("Failed to submit processing task")
		}
		logger.Infof(ctx, "Enqueued URL reparse task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, existing.ID)
		recordReparseStarted()

		return existing, nil
	}

	logger.Warnf(ctx, "Knowledge %s has no parseable content (no file, URL, or manual content)", knowledgeID)
	s.recordTerminalMutationFailure()
	existing.ParseStatus = types.ParseStatusFailed
	existing.ErrorMessage = "Knowledge has no parseable content"
	if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
		logger.Errorf(ctx, "Failed to persist unparseable knowledge state for %s: %v",
			secutils.SanitizeForLog(knowledgeID), err)
	}
	return existing, werrors.NewBadRequestError("Knowledge has no parseable content")
}

// resetKnowledgeForReparse makes the top-level knowledge state describe the
// new processing attempt rather than retaining terminal state from the
// previous one.
func resetKnowledgeForReparse(knowledge *types.Knowledge, kb *types.KnowledgeBase) {
	knowledge.ParseStatus = types.ParseStatusPending
	knowledge.EnableStatus = "disabled"
	knowledge.Description = ""
	knowledge.StructuredSummary = nil
	knowledge.SummaryStatus = types.SummaryStatusPending
	knowledge.ProcessedAt = nil
	knowledge.ErrorMessage = ""
	knowledge.EmbeddingModelID = kb.EmbeddingModelID
	// UpdateKnowledge deliberately omits pending_subtasks_count, so callers
	// must still persist this reset through an explicit column update.
	knowledge.PendingSubtasksCount = 0
}

// CancelKnowledgeParse marks an in-progress parse as cancelled by the user.
//
// Semantics (kept aligned with the existing deleting path, but partial work
// is preserved instead of cleaned up):
//   - parse_status is set to "cancelled"; partial chunks/index already written
//     to the database remain on disk. The user can re-trigger parsing via the
//     existing ReparseKnowledge API, which overwrites status back to pending.
//   - Any in-flight worker reads the new status at its next checkpoint and
//     bails (see processChunks / ProcessDocument / downstream handlers).
//   - The task inspector (if available) dequeues pending / scheduled / retry
//     tasks for this knowledge_id across all queues used by the pipeline
//     and signals active workers to stop. in-memory fixtures (no queue client) skips the
//     dequeue step — the checkpoint-based abort is the only stop signal there.
//   - Idempotent: re-calling on an already-cancelled row is a no-op.
//
// Errors:
//   - ParseStatusCompleted / ParseStatusFailed: the parse has already finished.
//   - ParseStatusDeleting: a delete is in progress; cancel cannot supersede it.
func (s *knowledgeService) CancelKnowledgeParse(
	ctx context.Context, knowledgeID string,
) (*types.Knowledge, error) {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	existing, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		logger.Errorf(ctx, "CancelKnowledgeParse: failed to load knowledge: %v", err)
		return nil, err
	}
	if existing == nil {
		return nil, werrors.NewNotFoundError("knowledge not found")
	}

	switch existing.ParseStatus {
	case types.ParseStatusCancelled:
		// Idempotent — still attempt the dequeue in case earlier calls
		// raced an enqueue, but skip the row update / span close path.
		s.dequeueKnowledgeTasks(ctx, knowledgeID)
		return existing, nil
	case types.ParseStatusCompleted, types.ParseStatusFailed:
		return nil, werrors.NewBadRequestError("解析已结束，无法取消")
	case types.ParseStatusDeleting:
		return nil, werrors.NewBadRequestError("知识正在删除中，无法取消解析")
	case types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing:
		// Cancellable. `finalizing` is the post-process fan-out window
		// where summary / question subtasks are still
		// running; cancel here stops the LLM cost they would burn.
	default:
		// Unknown status — let it through but log. Should never happen
		// outside test fixtures or hand-edited rows.
		logger.Warnf(ctx, "CancelKnowledgeParse: unexpected status %q for %s, proceeding",
			existing.ParseStatus, knowledgeID)
	}

	// Flip the row to cancelled and zero the enrichment counter in one
	// update so a late subtask FinalizeSubtask call can't race-promote
	// the row back to completed. Persisted partial data is left in
	// place — the user can reuse it on the next reparse attempt.
	now := time.Now()
	if err := s.repo.UpdateKnowledgeColumns(ctx, existing.ID, map[string]interface{}{
		"parse_status":           types.ParseStatusCancelled,
		"error_message":          "用户已取消解析",
		"pending_subtasks_count": 0,
		"updated_at":             now,
	}); err != nil {
		logger.Errorf(ctx, "CancelKnowledgeParse: failed to mark knowledge cancelled: %v", err)
		return nil, err
	}
	existing.ParseStatus = types.ParseStatusCancelled
	existing.ErrorMessage = "用户已取消解析"
	existing.PendingSubtasksCount = 0
	existing.UpdatedAt = now
	logger.Infof(ctx, "Knowledge %s marked as cancelled by user", knowledgeID)

	// Close the active attempt span tree so the UI stops showing "进行中"
	// for the cancelled run. AbortAttempt cascade-cancels every still-
	// running parsing and processing stage before closing the root, otherwise the trace
	// viewer would leave those striped/running bars hanging forever
	// because workers exit via their abort-guard without ever calling
	// FailSpan on their own subspan. Best-effort: nil tracker / missing
	// attempt no-ops.
	if attempt := s.tracker().LatestAttempt(ctx, knowledgeID); attempt > 0 {
		s.tracker().AbortAttempt(ctx, knowledgeID, attempt,
			"USER_CANCELLED", "用户已取消解析", "用户已取消解析")
	}

	// Best-effort dequeue. Failures here don't block the cancel — the
	// downstream tasks will still self-abort at their entry guards.
	s.dequeueKnowledgeTasks(ctx, knowledgeID)
	return existing, nil
}

// dequeueKnowledgeTasks asks the task inspector to remove any queued
// tasks for this knowledge and signal active workers to stop. Safe to
// call when the inspector is a no-op (in-memory fixtures).
func (s *knowledgeService) dequeueKnowledgeTasks(ctx context.Context, knowledgeID string) {
	if s.taskInspector == nil {
		return
	}
	if _, _, err := s.taskInspector.CancelTasksForKnowledge(ctx, knowledgeID); err != nil {
		logger.Warnf(ctx, "CancelKnowledgeParse: dequeue best-effort failed for %s: %v", knowledgeID, err)
	}
}

func (s *knowledgeService) ProcessManualUpdate(ctx context.Context, t *taskqueue.Task) error {
	var payload types.ManualProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "failed to unmarshal manual process task payload: %v", err)
		return nil
	}

	ctx = logger.WithRequestID(ctx, payload.RequestId)
	ctx = logger.WithField(ctx, "manual_process", payload.KnowledgeID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)

	tenantInfo, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get tenant: %v", err)
		return nil
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenantInfo)

	knowledge, err := s.repo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get knowledge: %v", err)
		return nil
	}
	if knowledge == nil {
		logger.Warnf(ctx, "ProcessManualUpdate: knowledge not found: %s", payload.KnowledgeID)
		return nil
	}

	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		if intake.Decision == "rejected" || intake.Decision == "error" {
			return nil
		}
		defer func() {
			if knowledge.ParseStatus == types.ParseStatusFailed {
				s.failIntake(ctx, knowledge)
			}
		}()
	}

	// Skip if already completed or being deleted
	if knowledge.ParseStatus == types.ParseStatusCompleted {
		logger.Infof(ctx, "ProcessManualUpdate: already completed, skipping: %s", payload.KnowledgeID)
		return nil
	}
	if knowledge.ParseStatus == types.ParseStatusDeleting {
		logger.Infof(ctx, "ProcessManualUpdate: being deleted, skipping: %s", payload.KnowledgeID)
		return nil
	}
	if knowledge.ParseStatus == types.ParseStatusCancelled {
		logger.Infof(ctx, "ProcessManualUpdate: cancelled by user, skipping: %s", payload.KnowledgeID)
		return nil
	}

	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, payload.KnowledgeBaseID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get knowledge base: %v", err)
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = fmt.Sprintf("failed to get knowledge base: %v", err)
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		return nil
	}

	// Re-check abort status right before marking processing — see the same
	// note in ProcessDocument for the cancel race this guards.
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		logger.Infof(ctx, "ProcessManualUpdate: knowledge aborted (%s), skipping: %s", status, knowledge.ID)
		return nil
	}
	// Update status to processing
	markKnowledgeProcessing(knowledge, time.Now())
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to update status to processing: %v", err)
		return nil
	}

	// Allocate an attempt and pass it through article processing and indexing
	// so all manual processing stages belong to this execution.
	attempt := 0
	if root, n, err := s.tracker().OpenAttempt(ctx, knowledge.ID); err == nil && root != nil {
		attempt = n
	} else if err != nil {
		logger.Warnf(ctx, "ProcessManualUpdate: OpenAttempt failed for %s: %v", knowledge.ID, err)
	}
	ctx = withAttempt(ctx, attempt)

	// Cleanup old resources (indexes and chunks) for update operations
	if payload.NeedCleanup {
		if err := s.cleanupKnowledgeResources(ctx, knowledge); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"knowledge_id": payload.KnowledgeID,
			})
			knowledge.ParseStatus = "failed"
			knowledge.ErrorMessage = fmt.Sprintf("failed to cleanup old resources: %v", err)
			knowledge.UpdatedAt = time.Now()
			s.repo.UpdateKnowledge(ctx, knowledge)
			return nil
		}
	}

	// Run manual processing (image resolution + chunking + embedding) synchronously within the worker
	return s.triggerManualProcessing(ctx, kb, knowledge, payload.Content, true)
}

// ProcessDocument handles PostgreSQL queue document processing tasks
func (s *knowledgeService) ProcessDocument(ctx context.Context, t *taskqueue.Task) (returnErr error) {
	var payload types.DocumentProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "failed to unmarshal document process task payload: %v", err)
		return nil
	}

	ctx = logger.WithRequestID(ctx, payload.RequestId)
	ctx = logger.WithField(ctx, "document_process", payload.KnowledgeID)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// 获取任务重试信息，用于判断是否是最后一次重试
	retryCount, _ := taskqueue.GetRetryCount(ctx)
	maxRetry, _ := taskqueue.GetMaxRetry(ctx)
	isLastRetry := retryCount >= maxRetry

	tenantInfo, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Errorf(ctx, "failed to get tenant: %v", err)
		return nil
	}
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenantInfo)

	logger.Infof(ctx, "Processing document task: knowledge_id=%s, file_path=%s, retry=%d/%d",
		payload.KnowledgeID, payload.FilePath, retryCount, maxRetry)

	// 幂等性检查：获取knowledge记录
	knowledge, err := s.repo.GetKnowledgeByID(ctx, payload.TenantID, payload.KnowledgeID)
	if err != nil {
		logger.Errorf(ctx, "failed to get knowledge: %v", err)
		return nil
	}

	if knowledge == nil {
		return nil
	}

	refetchingOriginal := knowledge.ProcessingMode == "summary_only" && (payload.URL != "" || payload.FileURL != "")

	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		if intake.Decision == "rejected" || intake.Decision == "error" {
			return nil
		}
		defer func() {
			if types.IsChatGPTDeferred(returnErr) {
				s.markArticleProcessingFailed(ctx, knowledge, returnErr)
				return
			}
			if returnErr != nil && refetchingOriginal {
				s.markArticleProcessingFailed(ctx, knowledge, fmt.Errorf("cannot refetch original article for reprocessing: %w", returnErr))
			}
			if returnErr != nil && knowledge.ErrorMessage == "" {
				knowledge.ErrorMessage = returnErr.Error()
			}
			if returnErr != nil || knowledge.ParseStatus == types.ParseStatusFailed {
				s.failIntake(ctx, knowledge)
			}
		}()
	}

	// 检查是否正在删除 / 已被用户取消 - 如果是则直接退出
	if knowledge.ParseStatus == types.ParseStatusDeleting {
		logger.Infof(ctx, "Knowledge is being deleted, aborting processing: %s", payload.KnowledgeID)
		return nil
	}
	if knowledge.ParseStatus == types.ParseStatusCancelled {
		logger.Infof(ctx, "Knowledge cancelled by user, aborting processing: %s", payload.KnowledgeID)
		return nil
	}

	// 检查任务状态 - 幂等性处理
	if knowledge.ParseStatus == types.ParseStatusCompleted {
		logger.Infof(ctx, "Document already completed, skipping: %s", payload.KnowledgeID)
		return nil // 幂等：已完成的任务直接返回
	}

	if knowledge.ParseStatus == types.ParseStatusFailed {
		// 检查是否可恢复（例如：超时、临时错误等）
		// 对于不可恢复的错误，直接返回
		logger.Warnf(
			ctx,
			"Document processing previously failed: %s, error: %s",
			payload.KnowledgeID,
			knowledge.ErrorMessage,
		)
		// 这里可以根据错误类型判断是否可恢复，暂时允许重试
	}

	// 检查是否有部分处理（有chunks但状态不是completed）
	if knowledge.ParseStatus != "completed" && knowledge.ParseStatus != "pending" &&
		knowledge.ParseStatus != "processing" {
		// 状态异常，记录日志但继续处理
		logger.Warnf(ctx, "Unexpected parse status: %s for knowledge: %s", knowledge.ParseStatus, payload.KnowledgeID)
	}

	// 获取知识库信息
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, payload.KnowledgeBaseID)
	if err != nil {
		logger.Errorf(ctx, "failed to get knowledge base: %v", err)
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = fmt.Sprintf("failed to get knowledge base: %v", err)
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		return nil
	}

	processOverrides, _ := knowledge.ProcessOverrides()
	eff := ResolveProcessConfig(kb, processOverrides)

	// Re-check abort status right before flipping to "processing" — closes
	// the race where the user cancels between the entry guard above and
	// this write (otherwise the worker would overwrite cancelled→processing
	// and downstream checkpoints would treat the run as live).
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		logger.Infof(ctx, "Knowledge aborted (%s) before marking processing: %s", status, knowledge.ID)
		return nil
	}
	markKnowledgeProcessing(knowledge, time.Now())
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		logger.Errorf(ctx, "failed to update knowledge status to processing: %v", err)
		return nil
	}

	// Resolve the attempt for span tracking. The enqueue site sets
	// payload.Attempt to a fresh number for the initial parse and to
	// max+1 for each user-initiated reparse. PostgreSQL queue retries within a
	// single user action keep the same payload (so retries record
	// onto the same attempt). For payloads predating this code we
	// fall back to OpenAttempt.
	attempt := payload.Attempt
	if attempt <= 0 {
		if root, n, err := s.tracker().OpenAttempt(ctx, knowledge.ID); err == nil && root != nil {
			attempt = n
		}
	}
	ctx = withAttempt(ctx, attempt)

	if payload.FilePath != "" && IsAudioType(payload.FileType) {
		knowledge.ParseStatus = types.ParseStatusFailed
		knowledge.ErrorMessage = fmt.Sprintf("不支持上传音频文件: %s", normalizeFileExtension(payload.FileType))
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		return nil
	}

	// 视频文件不再支持入库解析
	if payload.FilePath != "" && IsVideoType(payload.FileType) {
		logger.GetLogger(ctx).WithField("knowledge_id", knowledge.ID).
			Errorf("processDocument video not supported")
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = "暂不支持视频文件"
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		return nil
	}

	// Parse, resolve body images, review intake, then process the article once.
	var convertResult *types.ReadResult

	if payload.FileURL != "" {
		// file_url import: SSRF re-check (防 DNS 重绑定), download, persist, then delegate to convert()
		if err := secutils.ValidateURLForSSRF(payload.FileURL); err != nil {
			logger.Errorf(ctx, "File URL rejected for SSRF protection in ProcessDocument: %s, err: %v", payload.FileURL, err)
			knowledge.ParseStatus = "failed"
			knowledge.ErrorMessage = "File URL is not allowed for security reasons"
			knowledge.UpdatedAt = time.Now()
			s.repo.UpdateKnowledge(ctx, knowledge)
			return nil
		}

		resolvedFileName := payload.FileName
		resolvedFileType := payload.FileType
		contentBytes, err := downloadFileFromURL(ctx, payload.FileURL, &resolvedFileName, &resolvedFileType)
		if err != nil {
			logger.Errorf(ctx, "Failed to download file from URL: %s, error: %v", payload.FileURL, err)
			if isLastRetry {
				knowledge.ParseStatus = "failed"
				knowledge.ErrorMessage = err.Error()
				knowledge.UpdatedAt = time.Now()
				s.repo.UpdateKnowledge(ctx, knowledge)
			}
			return fmt.Errorf("failed to download file from URL: %w", err)
		}

		if resolvedFileType != "" && !isSupportedImportExtension(resolvedFileType) {
			logger.Errorf(ctx, "Unsupported file type resolved from file URL: %s", resolvedFileType)
			knowledge.ParseStatus = "failed"
			knowledge.ErrorMessage = fmt.Sprintf("unsupported file type: %s", resolvedFileType)
			knowledge.UpdatedAt = time.Now()
			s.repo.UpdateKnowledge(ctx, knowledge)
			return nil
		}

		if resolvedFileName != "" && knowledge.FileName == "" {
			knowledge.FileName = resolvedFileName
		}
		if resolvedFileType != "" && knowledge.FileType == "" {
			knowledge.FileType = resolvedFileType
			s.repo.UpdateKnowledge(ctx, knowledge)
		}

		fileSvc := s.resolveFileService(ctx, kb)
		filePath, err := fileSvc.SaveBytes(ctx, contentBytes, payload.TenantID, resolvedFileName, true)
		if err != nil {
			if isLastRetry {
				knowledge.ParseStatus = "failed"
				knowledge.ErrorMessage = err.Error()
				knowledge.UpdatedAt = time.Now()
				s.repo.UpdateKnowledge(ctx, knowledge)
			}
			return fmt.Errorf("failed to save downloaded file: %w", err)
		}

		knowledge.FilePath = filePath
		if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
			_ = fileSvc.DeleteFile(ctx, filePath)
			return err
		}
		payload.FilePath = filePath
		payload.FileName = resolvedFileName
		payload.FileType = resolvedFileType
		convertResult, err = s.convert(ctx, payload, kb, knowledge, eff, isLastRetry)
		if err != nil {
			return err
		}
		if convertResult == nil {
			return nil
		}
	} else if payload.URL != "" {
		// URL import
		convertResult, err = s.convert(ctx, payload, kb, knowledge, eff, isLastRetry)
		if err != nil {
			return err
		}
		if convertResult == nil {
			return nil
		}
		// Update knowledge title from extracted page title if not already set
		if knowledge.Title == "" || knowledge.Title == payload.URL {
			if extractedTitle := convertResult.Metadata["title"]; extractedTitle != "" {
				knowledge.Title = extractedTitle
				knowledge.UpdatedAt = time.Now()
				if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
					logger.Warnf(ctx, "Failed to update knowledge title from extracted page title: %v", err)
				} else {
					logger.Infof(ctx, "Updated knowledge title to extracted page title: %s", extractedTitle)
				}
			}
		}
	} else if len(payload.Passages) > 0 {
		convertResult = &types.ReadResult{MarkdownContent: strings.Join(payload.Passages, "\n\n")}
	} else {
		// File import
		convertResult, err = s.convert(ctx, payload, kb, knowledge, eff, isLastRetry)
		if err != nil {
			return err
		}
		if convertResult == nil {
			return nil
		}
	}

	refetchingOriginal = false

	// Step 2: Store images and update markdown references
	var storedImages []docparser.StoredImage

	if s.imageResolver != nil && convertResult != nil {
		fileSvc := s.resolveFileService(ctx, kb)
		tenantID, _ := ctx.Value(types.TenantIDContextKey).(uint64)
		updatedMarkdown, images, resolveErr := s.imageResolver.ResolveAndStore(ctx, convertResult, fileSvc, tenantID)
		if err := s.recordIntakeImages(ctx, knowledge, images); err != nil {
			return err
		}
		if resolveErr != nil {
			logger.Warnf(ctx, "Image resolution partially failed: %v", resolveErr)
			if intake, _ := knowledge.IntakeMetadata(); intake != nil {
				return fmt.Errorf("resolve intake images: %w", resolveErr)
			}
		}
		if updatedMarkdown != "" {
			convertResult.MarkdownContent = updatedMarkdown
		}
		storedImages = images

		// Resolve remote http(s) images (e.g. markdown external URLs) → download + upload to storage.
		// ResolveAndStore handles inline bytes and base64; ResolveRemoteImages handles http/https URLs.
		updatedContent, remoteImages, remoteErr := s.imageResolver.ResolveRemoteImages(ctx, convertResult.MarkdownContent, fileSvc, tenantID)
		if err := s.recordIntakeImages(ctx, knowledge, remoteImages); err != nil {
			return err
		}
		if remoteErr != nil {
			logger.Warnf(ctx, "Remote image resolution partially failed: %v", remoteErr)
			if intake, _ := knowledge.IntakeMetadata(); intake != nil {
				return fmt.Errorf("resolve intake images: %w", remoteErr)
			}
		}
		if updatedContent != "" {
			convertResult.MarkdownContent = updatedContent
		}
		if len(remoteImages) > 0 {
			logger.Infof(ctx, "Resolved %d remote images for knowledge %s", len(remoteImages), knowledge.ID)
			storedImages = append(storedImages, remoteImages...)
		}
		if intake, _ := knowledge.IntakeMetadata(); intake != nil && docparser.HasUnstoredImages(convertResult.MarkdownContent) {
			return fmt.Errorf("intake has unstored content images")
		}

		logger.Infof(ctx, "Resolved %d total images for knowledge %s", len(storedImages), knowledge.ID)
	}

	// Step 3: Split into chunks using Go chunker. Browser textareas normalize
	// pasted content to LF, so normalize uploaded source text before calculating
	// chunk boundaries as well.
	sanitizeReadResult(convertResult)
	convertResult.MarkdownContent = chunker.NormalizeLineEndings(convertResult.MarkdownContent)
	if err := hydrateMessageSource(knowledge, convertResult.Metadata); err != nil {
		return err
	}
	if intake, _ := knowledge.IntakeMetadata(); intake != nil && intake.Decision != "approved" {
		existing, err := s.checkMessageDuplicate(ctx, knowledge, convertResult.MarkdownContent)
		if err != nil {
			return err
		}
		if existing != nil {
			return s.stopDuplicateProcessing(ctx, knowledge)
		}
	}
	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		if docparser.HasUnstoredImages(convertResult.MarkdownContent) {
			return fmt.Errorf("intake has unstored content images")
		}
		if err := s.saveIntakeOriginal(ctx, kb, knowledge, convertResult.MarkdownContent, convertResult.Metadata); err != nil {
			return err
		}
	}

	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		rejected, reviewErr := s.reviewIntake(ctx, kb, knowledge, convertResult.MarkdownContent)
		if reviewErr != nil {
			return reviewErr
		}
		if rejected {
			return nil
		}
	}
	if isDataTableFileType(knowledge.FileType) {
		body, err := s.prepareTableProcessingBody(ctx, kb, knowledge)
		if err != nil {
			s.markArticleProcessingFailed(ctx, knowledge, err)
			return err
		}
		convertResult.MarkdownContent = body
	}
	if err := s.processArticleBody(ctx, kb, knowledge, convertResult.MarkdownContent, storedImages, eff); err != nil {
		s.markArticleProcessingFailed(ctx, knowledge, err)
		return err
	}

	return nil
}

// sanitizeReadResult protects every text field that can cross from a parser
// into the embedding, storage, or tracing layers. A parser may return a Go
// string containing arbitrary bytes even though the string type itself does
// not enforce UTF-8 validity.
func sanitizeReadResult(result *types.ReadResult) {
	if result == nil {
		return
	}
	result.MarkdownContent = secutils.CleanInvalidUTF8(result.MarkdownContent)
	result.ImageDirPath = secutils.CleanInvalidUTF8(result.ImageDirPath)
	result.Error = secutils.CleanInvalidUTF8(result.Error)
	for key, value := range result.Metadata {
		result.Metadata[key] = secutils.CleanInvalidUTF8(value)
	}
	for i := range result.ImageRefs {
		result.ImageRefs[i].Filename = secutils.CleanInvalidUTF8(result.ImageRefs[i].Filename)
		result.ImageRefs[i].OriginalRef = secutils.CleanInvalidUTF8(result.ImageRefs[i].OriginalRef)
		result.ImageRefs[i].MimeType = secutils.CleanInvalidUTF8(result.ImageRefs[i].MimeType)
		result.ImageRefs[i].StorageKey = secutils.CleanInvalidUTF8(result.ImageRefs[i].StorageKey)
	}
}

// convert handles both file and URL reading using a unified ReadRequest.
func (s *knowledgeService) convert(
	ctx context.Context,
	payload types.DocumentProcessPayload,
	kb *types.KnowledgeBase,
	knowledge *types.Knowledge,
	eff types.EffectiveProcessConfig,
	isLastRetry bool,
) (*types.ReadResult, error) {
	// Stage tracking: docreader. Mark the stage as running here so the
	// timeline reflects "DocReader" the moment a worker picks the task
	// up — before that, the stage stays "pending" from the initial
	// upload. Failure/skip transitions are emitted at the specific
	// failure points below; success is emitted at the bottom.
	docInput := types.JSONMap{
		"file_name": payload.FileName,
		"file_type": payload.FileType,
		"is_url":    payload.URL != "",
	}
	if payload.URL != "" {
		docInput["url"] = payload.URL
	}
	s.beginStage(ctx, knowledge.ID, types.StageDocReader, docInput)
	isURL := payload.URL != ""
	fileType := payload.FileType
	tenantOverrides := s.getParserEngineOverridesFromContext(ctx)
	var uploadOverrides map[string]string
	if processOverrides, err := knowledge.ProcessOverrides(); err == nil && processOverrides != nil {
		uploadOverrides = processOverrides.ParserEngineOverrides
	}
	mergedOverrides := MergeParserEngineOverrides(tenantOverrides, uploadOverrides)
	if err := validateParserEngineOverrideURLs(mergedOverrides); err != nil {
		logger.Errorf(ctx, "Parser endpoint rejected for SSRF protection: %v", err)
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = "Parser endpoint is not allowed for security reasons"
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		s.failStage(ctx, knowledge.ID, types.StageDocReader,
			werrors.ErrCodeDocReaderParseFailed, knowledge.ErrorMessage, err)
		return nil, nil
	}

	if isURL {
		if err := secutils.ValidateURLForSSRF(payload.URL); err != nil {
			logger.Errorf(ctx, "URL rejected for SSRF protection: %s, err: %v", payload.URL, err)
			knowledge.ParseStatus = "failed"
			knowledge.ErrorMessage = "URL is not allowed for security reasons"
			knowledge.UpdatedAt = time.Now()
			s.repo.UpdateKnowledge(ctx, knowledge)
			s.failStage(ctx, knowledge.ID, types.StageDocReader,
				werrors.ErrCodeDocReaderParseFailed, "URL rejected for security reasons", err)
			return nil, nil
		}
	}

	parserEngine := eff.ChunkingConfig.ResolveParserEngine(fileType)
	if isURL {
		parserEngine = eff.ChunkingConfig.ResolveParserEngine("url")
	}

	logger.Infof(ctx, "[convert] kb=%s fileType=%s isURL=%v engine=%q rules=%+v",
		kb.ID, fileType, isURL, parserEngine, eff.ChunkingConfig.ParserEngineRules)

	reader, readerErr := docparser.NewReader(ctx, parserEngine, fileType, isURL, docparser.ReaderDeps{Overrides: mergedOverrides})
	if readerErr != nil {
		logger.Errorf(ctx, "[convert] no doc reader for kb=%s knowledge=%s fileType=%s engine=%q isURL=%v",
			kb.ID, knowledge.ID, fileType, parserEngine, isURL)
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = readerErr.Error()
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		s.failStage(ctx, knowledge.ID, types.StageDocReader,
			werrors.ErrCodeDocReaderUnavailable, knowledge.ErrorMessage, nil)
		return nil, nil
	}

	req := &types.ReadRequest{
		URL:                   payload.URL,
		Title:                 knowledge.Title,
		ParserEngine:          parserEngine,
		RequestID:             payload.RequestId,
		ParserEngineOverrides: mergedOverrides,
	}

	if !isURL {
		fileReader, err := s.resolveFileServiceForPath(ctx, kb, payload.FilePath).GetFile(ctx, payload.FilePath)
		if err != nil {
			s.failStage(ctx, knowledge.ID, types.StageDocReader,
				werrors.ErrCodeDocReaderParseFailed, "failed to get file", err)
			return s.failKnowledge(ctx, knowledge, isLastRetry, "failed to get file: %v", err)
		}
		defer fileReader.Close()
		contentBytes, err := io.ReadAll(fileReader)
		if err != nil {
			s.failStage(ctx, knowledge.ID, types.StageDocReader,
				werrors.ErrCodeDocReaderParseFailed, "failed to read file", err)
			return s.failKnowledge(ctx, knowledge, isLastRetry, "failed to read file: %v", err)
		}
		req.FileContent = contentBytes
		req.FileName = payload.FileName
		req.FileType = fileType
	}

	result, err := s.callDocReaderWithTimeout(ctx, reader, req)
	if err != nil {
		var fetchErr *webfetch.FetchError
		if isURL && errors.As(err, &fetchErr) &&
			(fetchErr.Code == webfetch.ErrorJavaScriptRequired || fetchErr.Code == webfetch.ErrorEmptyContent) {
			knowledge.ParseStatus = types.ParseStatusFailed
			knowledge.ErrorMessage = fetchErr.Error()
			knowledge.UpdatedAt = time.Now()
			if updateErr := s.repo.UpdateKnowledge(ctx, knowledge); updateErr != nil {
				return nil, updateErr
			}
			s.failStage(ctx, knowledge.ID, types.StageDocReader, string(fetchErr.Code), knowledge.ErrorMessage, err)
			return nil, nil
		}
		// Distinguish DocReader timeout (a knowable user-facing
		// failure) from generic read errors so the UI can suggest
		// "split this large file" specifically when relevant.
		code := werrors.ErrCodeDocReaderParseFailed
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "parser call timeout") {
			code = werrors.ErrCodeDocReaderTimeout
		}
		s.failStage(ctx, knowledge.ID, types.StageDocReader,
			code, "document read failed", err)
		return s.failKnowledge(ctx, knowledge, isLastRetry, "document read failed: %v", err)
	}
	sanitizeReadResult(result)
	if err := extractIntakeOriginalLink(knowledge, result.MarkdownContent, req.FileContent, fileType, result.Metadata); err != nil {
		return nil, err
	}
	if result.Error != "" {
		logger.Errorf(ctx, "[convert] parser returned error kb=%s knowledge=%s file=%q type=%s engine=%q: %s",
			kb.ID, knowledge.ID, req.FileName, fileType, parserEngine, result.Error)
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = result.Error
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
		s.failStage(ctx, knowledge.ID, types.StageDocReader,
			werrors.ErrCodeDocReaderParseFailed, result.Error, nil)
		return nil, nil
	}
	docOutput := types.JSONMap{
		"text_length":  len(result.MarkdownContent),
		"images_found": len(result.ImageRefs),
	}
	if pages := result.Metadata["pages"]; pages != "" {
		docOutput["pages"] = pages
	}
	s.endStage(ctx, knowledge.ID, types.StageDocReader, docOutput)
	return result, nil
}

// callDocReaderWithTimeout wraps the parser call in a child context whose
// deadline is min(parent_deadline, ParserCallTimeout). Without this cap,
// a hung parser (network partition, GC pause, OCR runaway) silently
// burns the whole DocumentProcessTimeout budget and pins a worker for hours
// — the #1 cause of "knowledge stuck in processing" reports.
//
// On timeout we annotate the error so retries / dead-letter consumers can
// distinguish "parser was slow" from "parser returned an error".
func (s *knowledgeService) callDocReaderWithTimeout(
	ctx context.Context, reader interfaces.DocReader, req *types.ReadRequest,
) (*types.ReadResult, error) {
	timeout := 30 * time.Minute
	if s.config != nil && s.config.KnowledgeBase != nil && s.config.KnowledgeBase.ParserCallTimeout > 0 {
		timeout = s.config.KnowledgeBase.ParserCallTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	result, err := reader.Read(callCtx, req)
	elapsed := time.Since(start)
	if err != nil {
		// Promote DeadlineExceeded into a clearer message; retain underlying
		// error via %w so errors.Is(callCtx.Err(), context.DeadlineExceeded)
		// still works for upstream classification.
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logger.Errorf(ctx, "[convert] parser call timed out after %s (limit %s) for %q",
				elapsed, timeout, req.FileName)
			return nil, fmt.Errorf("parser call timeout after %s: %w", timeout, err)
		}
		return nil, err
	}
	logger.Infof(ctx, "[convert] parser call ok in %s for %q", elapsed, req.FileName)
	return result, nil
}

// isLikelyRateLimitError performs a fuzzy classification of an error as a
// rate-limit / quota / backpressure failure. We only need a hint — the
// caller maps to one of two error_codes so the UI can offer "retry later"
// vs. "fix configuration" advice. False positives are harmless (the
// detail is preserved in error_detail anyway).
func isLikelyRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"rate limit", "ratelimit", "429", "too many requests", "quota"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// failKnowledge marks knowledge as failed (only on last retry) and returns an error.
func (s *knowledgeService) failKnowledge(
	ctx context.Context,
	knowledge *types.Knowledge,
	isLastRetry bool,
	format string,
	args ...interface{},
) (*types.ReadResult, error) {
	errMsg := fmt.Sprintf(format, args...)
	if isLastRetry {
		knowledge.ParseStatus = "failed"
		knowledge.ErrorMessage = errMsg
		knowledge.UpdatedAt = time.Now()
		s.repo.UpdateKnowledge(ctx, knowledge)
	}
	return nil, fmt.Errorf(format, args...)
}

func (s *knowledgeService) ProcessKnowledgeListReparse(ctx context.Context, t *taskqueue.Task) error {
	var payload types.KnowledgeListReparsePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal knowledge list reparse payload: %v", err)
		return err
	}
	ctx = payload.Initiator.Apply(ctx)

	logger.Infof(ctx, "Processing knowledge list reparse task for %d knowledge items", len(payload.KnowledgeIDs))

	tenant, err := s.tenantRepo.GetTenantByID(ctx, payload.TenantID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get tenant %d: %v", payload.TenantID, err)
		return err
	}

	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)

	outcome, err := runKnowledgeListReparseSubmissions(payload.KnowledgeIDs, func(id string) error {
		_, err := s.ReparseKnowledge(ctx, id, payload.ProcessConfig)
		return err
	})
	logger.Infof(ctx, "Knowledge list reparse task finished: %d submitted, %d failed",
		outcome.Submitted, outcome.Failed)
	return err
}

type knowledgeListReparseOutcome struct {
	Submitted int
	Failed    int
}

// runKnowledgeListReparseSubmissions attempts every item so one bad document
// cannot block the remainder of the batch. A partial failure is non-retryable:
// retrying the wrapper task would destructively reparse items that were already
// submitted successfully. Failed rows remain selectable for an explicit retry.
func runKnowledgeListReparseSubmissions(
	ids []string,
	submit func(string) error,
) (knowledgeListReparseOutcome, error) {
	var outcome knowledgeListReparseOutcome
	failures := make([]error, 0)
	for _, id := range ids {
		if err := submit(id); err != nil {
			failures = append(failures, fmt.Errorf("knowledge %s: %w", secutils.SanitizeForLog(id), err))
			outcome.Failed++
			continue
		}
		outcome.Submitted++
	}
	if len(failures) == 0 {
		return outcome, nil
	}
	return outcome, fmt.Errorf(
		"%w: batch reparse submitted %d item(s) and failed %d: %w",
		taskqueue.SkipRetry, outcome.Submitted, outcome.Failed, errors.Join(failures...),
	)
}
