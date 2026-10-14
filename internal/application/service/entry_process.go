package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	taskqueue "reliquary/internal/queue"

	"github.com/google/uuid"

	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"

	"reliquary/internal/logger"
	"reliquary/internal/models/embedding"
	"reliquary/internal/sourceloc"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

// ProcessChunksOptions contains options for processing chunks
type ProcessChunksOptions struct {
	Body string
	// ParentChunks holds parent chunk data when parent-child chunking is enabled.
	// When set, the chunks passed to processChunks are child chunks, and each
	// child's ParentIndex references an entry in this slice.
	ParentChunks []types.ParsedParentChunk
}

// finalizeIndexedEntryState records the completed synchronous processing attempt.
func finalizeIndexedEntryState(entry *types.Entry, totalStorageSize int64, now time.Time) {
	entry.ParseStatus = types.ParseStatusCompleted
	entry.SummaryStatus = types.SummaryStatusCompleted
	entry.ErrorMessage = ""
	entry.EnableStatus = "enabled"
	entry.StorageSize = totalStorageSize
	entry.ProcessedAt = &now
	entry.UpdatedAt = now
}

// markEntryProcessing makes the top-level entry state describe the
// attempt that is starting now. Clearing error_message is part of that: the
// column is only meaningful for the attempt that produced it, so a row
// re-entering processing must not keep surfacing the previous attempt's
// failure while it is visibly running again.
func markEntryProcessing(entry *types.Entry, now time.Time) {
	entry.ParseStatus = types.ParseStatusProcessing
	entry.ErrorMessage = ""
	entry.UpdatedAt = now
}

// buildSplitterConfig creates a SplitterConfig with fallbacks from a Library.
// Defaults mirror chunker.DefaultChunkSize / DefaultChunkOverlap so behavior is
// identical whether callers come through this path or invoke the chunker
// directly with a zero-value config.
func buildSplitterConfig(library *types.Library) chunker.SplitterConfig {
	return buildSplitterConfigFromChunking(library.ChunkingConfig)
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

// processChunks processes chunks and creates embeddings for entry content
func (s *entryService) processChunks(ctx context.Context,
	library *types.Library, entry *types.Entry, chunks []types.ParsedChunk,
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

	// Check if entry is being deleted/cancelled before processing.
	// Both statuses short-circuit identically here — there's nothing to clean
	// up yet so the branch is purely "stop early".
	if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
		logger.Infof(ctx, "Entry aborted (%s), skipping chunk processing: %s", status, entry.ID)
		return
	}

	// Get embedding model for vectorization — only needed when vector/keyword indexing is enabled
	var embeddingModel embedding.Embedder
	var vectorErr error
	if library.NeedsEmbeddingModel() {
		var err error
		embeddingModel, err = GetTierEmbeddingModel(ctx, s.db, s.modelService, library)
		if err != nil {
			logger.GetLogger(ctx).WithField("error", err).Errorf("processChunks get embedding model failed")
			vectorErr = err
		}
		if embeddingModel != nil {
			entry.EmbeddingModelID = embeddingModel.GetModelID()
		}
	} else {
		logger.Infof(ctx, "Vector/keyword indexing disabled for Library %s, skipping embedding model", library.ID)
	}

	// 幂等性处理：清理旧的chunks和索引数据，避免重复数据
	logger.Infof(ctx, "Cleaning up existing chunks and index data for entry: %s", entry.ID)

	// 删除旧的chunks
	if err := s.chunkService.DeleteChunksByEntryID(ctx, entry.ID); err != nil {
		logger.Warnf(ctx, "Failed to delete existing chunks (may not exist): %v", err)
		// 不返回错误，继续处理（可能没有旧数据）
	}

	// 删除旧的索引数据 — only when vector/keyword indexing is enabled
	spaceInfo := ctx.Value(types.SpaceInfoContextKey).(*types.Space)
	var err error
	retrieveEngine := s.retrieveEngine
	if embeddingModel != nil {
		if err := retrieveEngine.DeleteByEntryIDList(ctx, []string{entry.ID}, embeddingModel.GetDimensions(), entry.Type); err != nil {
			logger.Warnf(ctx, "Failed to delete existing index data (may not exist): %v", err)
			// 不返回错误，继续处理（可能没有旧数据）
		} else {
			logger.Infof(ctx, "Successfully deleted existing index data for entry: %s", entry.ID)
		}
	}

	logger.Infof(ctx, "Cleanup completed, starting to process new chunks")

	// ========== DocReader 解析结果日志 ==========
	logger.Infof(ctx, "[DocReader] ========== 解析结果概览 ==========")
	logger.Infof(ctx, "[DocReader] 知识ID: %s, 库ID: %s", entry.ID, entry.LibraryID)
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
				ID:         uuid.New().String(),
				SpaceID:    entry.SpaceID,
				EntryID:    entry.ID,
				LibraryID:  entry.LibraryID,
				Content:    pc.Content,
				ChunkIndex: pc.Seq,
				IsEnabled:  true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				StartAt:    pc.Start,
				EndAt:      pc.End,
				ChunkType:  types.ChunkTypeParentText,
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
			ID:            uuid.New().String(),
			SpaceID:       entry.SpaceID,
			EntryID:       entry.ID,
			LibraryID:     entry.LibraryID,
			Content:       chunkData.Content,
			ContextHeader: chunkData.ContextHeader,
			ChunkIndex:    int(chunkData.Seq),
			IsEnabled:     true,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			StartAt:       int(chunkData.Start),
			EndAt:         int(chunkData.End),
			ChunkType:     types.ChunkTypeText,
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

	// Check if entry is being deleted/cancelled before writing chunks.
	// Nothing has been persisted yet, so both branches just bail.
	if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
		logger.Infof(ctx, "Entry aborted (%s), skipping chunk write: %s", status, entry.ID)
		return
	}

	// Save chunks to database — ALWAYS, regardless of indexing strategy.
	// Chunks are needed for entry context and summary generation
	// even when vector/keyword indexing is disabled.
	s.beginStage(ctx, entry.ID, types.StageChunking, types.JSONMap{
		"chunks_planned": len(insertChunks),
	})
	sourceloc.Assign(options.Body, insertChunks)
	if err := s.chunkService.CreateChunks(ctx, insertChunks); err != nil {
		entry.ParseStatus = types.ParseStatusFailed
		entry.ErrorMessage = err.Error()
		entry.UpdatedAt = time.Now()
		s.repo.UpdateEntry(ctx, entry)
		s.failStage(ctx, entry.ID, types.StageChunking,
			werrors.ErrCodeChunkingFailed, "create chunks failed", err)
		return
	}
	totalChunkChars := 0
	for _, c := range insertChunks {
		totalChunkChars += len(c.Content)
	}
	s.endStage(ctx, entry.ID, types.StageChunking, types.JSONMap{
		"chunks_written":   len(insertChunks),
		"total_text_chars": totalChunkChars,
	})

	// Create index information and perform vector indexing, only when vector/keyword is enabled.
	// Chunks are ALWAYS saved to DB (above) because entry context needs them even without vector indexing.
	var totalStorageSize int64
	if library.NeedsEmbeddingModel() && embeddingModel != nil {
		embedInput := types.JSONMap{
			"chunks_to_embed": len(textChunks),
			"model_id":        library.EmbeddingModelID,
		}
		if dim := embeddingModel.GetDimensions(); dim > 0 {
			embedInput["dim"] = dim
		}
		s.beginStage(ctx, entry.ID, types.StageEmbedding, embedInput)
		// Create index information, only for child/flat chunks, NOT parent chunks.
		// Parent chunks are stored for context retrieval but do not need vector embeddings.
		// Supply the article source prefix only to the vector input.
		indexInfoList := make([]*types.IndexInfo, 0, len(textChunks))
		for _, chunk := range textChunks {
			// Heading breadcrumbs remain stored separately. Vector inputs use
			// exactly one article prefix line followed by the chunk body.
			indexContent := buildEntryIndexContent(entry, chunk.Content)
			indexInfoList = append(indexInfoList, &types.IndexInfo{
				Content:    indexContent,
				SourceID:   chunk.ID,
				SourceType: types.ChunkSourceType,
				ChunkID:    chunk.ID,
				EntryID:    entry.ID,
				LibraryID:  entry.LibraryID,
				IsEnabled:  true,
			})
		}

		// Calculate storage size required for embeddings
		totalStorageSize = retrieveEngine.EstimateStorageSize(ctx, embeddingModel, indexInfoList)

		// Check again before batch indexing (heavy operation).
		// deleting → row is going away anyway, drop the chunks we just wrote.
		// cancelled → user wants to keep what was already persisted, just stop.
		if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
			logger.Infof(ctx, "Entry aborted (%s) before indexing: %s", status, entry.ID)
			if status == types.ParseStatusDeleting {
				if err := s.chunkService.DeleteChunksByEntryID(ctx, entry.ID); err != nil {
					logger.Warnf(ctx, "Failed to cleanup chunks after deletion detected: %v", err)
				}
			}
			return
		}

		err = retrieveEngine.BatchIndex(ctx, embeddingModel, indexInfoList)
		if err != nil {
			vectorErr = err
			s.skipStage(ctx, entry.ID, types.StageEmbedding, "vector_unavailable")
		} else {
			s.endStage(ctx, entry.ID, types.StageEmbedding, types.JSONMap{"vectors_written": len(indexInfoList), "storage_bytes": totalStorageSize})
		}

		// Final check before marking as completed.
		// deleting → drop chunks+index we just wrote.
		// cancelled → keep persisted data; the row stays in cancelled status
		// and downstream stages skip via the entry guards.
		if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
			logger.Infof(ctx, "Entry aborted (%s) after indexing: %s", status, entry.ID)
			if status == types.ParseStatusDeleting {
				if err := s.chunkService.DeleteChunksByEntryID(ctx, entry.ID); err != nil {
					logger.Warnf(ctx, "Failed to cleanup chunks after deletion detected: %v", err)
				}
				if err := retrieveEngine.DeleteByEntryIDList(ctx, []string{entry.ID}, embeddingModel.GetDimensions(), library.Type); err != nil {
					logger.Warnf(ctx, "Failed to cleanup index after deletion detected: %v", err)
				}
			}
			return
		}
	} else {
		logger.Infof(ctx, "Vector/keyword indexing disabled for Library %s, skipping BatchIndex", library.ID)
		s.skipStage(ctx, entry.ID, types.StageEmbedding, "skipped")
	}

	if vectorErr != nil {
		if err := s.deferEntryVectors(ctx, entry); err != nil {
			entry.ParseStatus = types.ParseStatusFailed
			entry.ErrorMessage = err.Error()
			_ = s.repo.UpdateEntry(ctx, entry)
			return
		}
	}

	now := time.Now()
	finalizeIndexedEntryState(entry, totalStorageSize, now)
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		entry.ParseStatus = types.ParseStatusFailed
		entry.ErrorMessage = err.Error()
	}

	// Update space's storage usage
	spaceInfo.StorageUsed += totalStorageSize
	if err := s.spaceRepo.AdjustStorageUsed(ctx, spaceInfo.ID, totalStorageSize); err != nil {
		logger.GetLogger(ctx).WithField("error", err).Errorf("processChunks update space storage used failed")
	}
	logger.GetLogger(ctx).Infof("processChunks successfully")
}

func (s *entryService) ReparseEntry(ctx context.Context, entryID string, overrides *types.EntryProcessOverrides) (entry *types.Entry, err error) {
	err = s.withTaskObject(ctx, entryID, func(held context.Context) error { entry, err = s.reparseEntry(held, entryID, overrides); return err })
	if err == nil {
		err = populateEntryLibraryNames(ctx, s.db, []*types.Entry{entry})
	}
	return entry, err
}

func (s *entryService) reparseEntry(
	ctx context.Context,
	entryID string,
	processOverrides *types.EntryProcessOverrides,
) (*types.Entry, error) {
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			return copy.ReparseEntry(ctx, entryID, processOverrides)
		})
	}

	logger.Info(ctx, "Start re-parsing entry")

	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	existing, err := s.repo.GetEntryByID(ctx, spaceID, entryID)
	if err != nil {
		logger.Errorf(ctx, "Failed to load entry: %v", err)
		return nil, err
	}
	if existing.SourceKind != "" && existing.SourceKind != "article" {
		return nil, werrors.NewBadRequestError("vault documents are indexed directly from their files")
	}

	if intake, _ := existing.IntakeMetadata(); intake != nil && (intake.Decision == "rejected" || intake.Decision == "error") {
		return nil, werrors.NewBadRequestError("use the intake review action for this message")
	}

	// Get library configuration
	library, err := s.libraryService.GetLibraryByID(ctx, existing.LibraryID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get library for reparse: %v", err)
		return nil, err
	}

	// When the caller supplies new overrides (e.g. via the reparse confirm
	// dialog), persist the Markdown processing choices
	// to metadata so both this call's enqueue and the worker re-read the same
	// config. nil keeps whatever was stored at upload time.
	if processOverrides != nil {
		if err := existing.SetProcessOverrides(processOverrides); err != nil {
			logger.Errorf(ctx, "Failed to set process overrides on reparse: %v", err)
			return nil, err
		}
		if err := s.repo.UpdateEntryColumn(ctx, existing.ID, "metadata", existing.Metadata); err != nil {
			logger.Errorf(ctx, "Failed to persist process overrides on reparse: %v", err)
			return nil, err
		}
	}

	body, hasBody, err := s.reprocessStoredBody(ctx, existing)
	if err != nil {
		return nil, err
	}
	if !hasBody {
		return s.markReprocessContentMissing(ctx, existing)
	}
	// The worker opens the attempt when execution begins, after queue delivery.
	if hasBody {
		resetEntryForReparse(existing, library)
		if err := s.repo.UpdateEntry(ctx, existing); err != nil {
			return nil, err
		}
		if _, err := s.enqueueManualProcessing(ctx, existing, body, true); err != nil {
			s.markEntryEnqueueFailed(ctx, existing)
			return existing, err
		}
		return existing, nil
	}
	return existing, nil
}

// resetEntryForReparse makes the top-level entry state describe the
// new processing attempt rather than retaining terminal state from the
// previous one.
func resetEntryForReparse(entry *types.Entry, library *types.Library) {
	entry.ParseStatus = types.ParseStatusPending
	entry.EnableStatus = "disabled"
	entry.Description = ""
	entry.StructuredSummary = nil
	entry.SummaryStatus = types.SummaryStatusPending
	entry.ProcessedAt = nil
	entry.ErrorMessage = ""
	entry.EmbeddingModelID = library.EmbeddingModelID
	// UpdateEntry deliberately omits pending_subtasks_count, so callers
	// must still persist this reset through an explicit column update.
	entry.PendingSubtasksCount = 0
}

// CancelEntryParse marks an in-progress parse as cancelled by the user.
//
// Semantics (kept aligned with the existing deleting path, but partial work
// is preserved instead of cleaned up):
//   - parse_status is set to "cancelled"; partial chunks/index already written
//     to the database remain on disk. The user can re-trigger parsing via the
//     existing ReparseEntry API, which overwrites status back to pending.
//   - Any in-flight worker reads the new status at its next checkpoint and
//     bails (see processChunks / ProcessDocument / downstream handlers).
//   - The task inspector (if available) dequeues pending / scheduled / retry
//     tasks for this entry_id across all queues used by the pipeline
//     and signals active workers to stop. in-memory fixtures (no queue client) skips the
//     dequeue step — the checkpoint-based abort is the only stop signal there.
//   - Idempotent: re-calling on an already-cancelled row is a no-op.
//
// Errors:
//   - Completed, or failed without queued work: processing has finished.
//   - ParseStatusDeleting: a delete is in progress; cancel cannot supersede it.
func (s *entryService) CancelEntryParse(ctx context.Context, entryID string) (entry *types.Entry, err error) {
	err = s.withTaskObject(ctx, entryID, func(held context.Context) error { entry, err = s.cancelEntryParse(held, entryID); return err })
	if err == nil {
		err = populateEntryLibraryNames(ctx, s.db, []*types.Entry{entry})
	}
	return entry, err
}

func (s *entryService) cancelEntryParse(
	ctx context.Context, entryID string,
) (*types.Entry, error) {
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	existing, err := s.repo.GetEntryByID(ctx, spaceID, entryID)
	if err != nil {
		logger.Errorf(ctx, "CancelEntryParse: failed to load entry: %v", err)
		return nil, err
	}
	if existing == nil {
		return nil, werrors.NewNotFoundError("entry not found")
	}

	switch existing.ParseStatus {
	case types.ParseStatusCancelled:
		// Idempotent — still attempt the dequeue in case earlier calls
		// raced an enqueue, but skip the row update / span close path.
		s.dequeueEntryTasks(ctx, entryID)
		return existing, nil
	case types.ParseStatusCompleted:
		return nil, werrors.NewBadRequestError("处理已结束，无法取消")
	case types.ParseStatusFailed:
		// A failed attempt can still own a queued retry. Cancel that work too.
		if s.taskInspector == nil {
			return nil, werrors.NewBadRequestError("处理已结束，无法取消")
		}
		queued, queryErr := s.taskInspector.HasQueuedTasksForEntry(ctx, entryID)
		if queryErr != nil {
			return nil, queryErr
		}
		if !queued {
			return nil, werrors.NewBadRequestError("处理已结束，无法取消")
		}
	case types.ParseStatusDeleting:
		return nil, werrors.NewBadRequestError("知识正在删除中，无法取消解析")
	case types.ParseStatusPending, types.ParseStatusProcessing, types.ParseStatusFinalizing:
		// Cancellable. `finalizing` is the post-process fan-out window
		// where summary / question subtasks are still
		// running; cancel here stops the LLM cost they would burn.
	default:
		// Unknown status — let it through but log. Should never happen
		// outside test fixtures or hand-edited rows.
		logger.Warnf(ctx, "CancelEntryParse: unexpected status %q for %s, proceeding",
			existing.ParseStatus, entryID)
	}

	// Flip the row to cancelled and zero the enrichment counter in one
	// update so a late subtask FinalizeSubtask call can't race-promote
	// the row back to completed. Persisted partial data is left in
	// place — the user can reuse it on the next reparse attempt.
	now := time.Now()
	if err := s.repo.UpdateEntryColumns(ctx, existing.ID, map[string]interface{}{
		"parse_status":           types.ParseStatusCancelled,
		"error_message":          "用户已取消解析",
		"pending_subtasks_count": 0,
		"updated_at":             now,
	}); err != nil {
		logger.Errorf(ctx, "CancelEntryParse: failed to mark entry cancelled: %v", err)
		return nil, err
	}
	existing.ParseStatus = types.ParseStatusCancelled
	existing.ErrorMessage = "用户已取消解析"
	existing.PendingSubtasksCount = 0
	existing.UpdatedAt = now
	logger.Infof(ctx, "Entry %s marked as cancelled by user", entryID)

	// Close the active attempt span tree so the UI stops showing "进行中"
	// for the cancelled run. AbortAttempt cascade-cancels every still-
	// running parsing and processing stage before closing the root, otherwise the trace
	// viewer would leave those striped/running bars hanging forever
	// because workers exit via their abort-guard without ever calling
	// FailSpan on their own subspan. Best-effort: nil tracker / missing
	// attempt no-ops.
	if attempt := s.tracker().LatestAttempt(ctx, entryID); attempt > 0 {
		s.tracker().AbortAttempt(ctx, entryID, attempt,
			"USER_CANCELLED", "用户已取消解析", "用户已取消解析")
	}

	// Best-effort dequeue. Failures here don't block the cancel — the
	// downstream tasks will still self-abort at their entry guards.
	s.dequeueEntryTasks(ctx, entryID)
	return existing, nil
}

// dequeueEntryTasks asks the task inspector to remove any queued
// tasks for this entry and signal active workers to stop. Safe to
// call when the inspector is a no-op (in-memory fixtures).
func (s *entryService) dequeueEntryTasks(ctx context.Context, entryID string) {
	if s.taskInspector == nil {
		return
	}
	if _, _, err := s.taskInspector.CancelTasksForEntry(ctx, entryID); err != nil {
		logger.Warnf(ctx, "CancelEntryParse: dequeue best-effort failed for %s: %v", entryID, err)
	}
}

func (s *entryService) ProcessManualUpdate(ctx context.Context, t *taskqueue.Task) error {
	var payload types.ManualProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "failed to unmarshal manual process task payload: %v", err)
		return nil
	}

	ctx = logger.WithRequestID(ctx, payload.RequestId)
	ctx = logger.WithField(ctx, "manual_process", payload.EntryID)
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)

	spaceInfo, err := s.spaceRepo.GetSpaceByID(ctx, payload.SpaceID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get space: %v", err)
		return nil
	}
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, spaceInfo)

	entry, err := s.repo.GetEntryByID(ctx, payload.SpaceID, payload.EntryID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get entry: %v", err)
		return nil
	}
	if entry == nil {
		logger.Warnf(ctx, "ProcessManualUpdate: entry not found: %s", payload.EntryID)
		return nil
	}

	if intake, _ := entry.IntakeMetadata(); intake != nil {
		if intake.Decision == "rejected" || intake.Decision == "error" {
			return nil
		}
		defer func() {
			if entry.ParseStatus == types.ParseStatusFailed {
				s.failIntake(ctx, entry)
			}
		}()
	}

	// Skip if already completed or being deleted
	if entry.ParseStatus == types.ParseStatusCompleted {
		logger.Infof(ctx, "ProcessManualUpdate: already completed, skipping: %s", payload.EntryID)
		return nil
	}
	if entry.ParseStatus == types.ParseStatusDeleting {
		logger.Infof(ctx, "ProcessManualUpdate: being deleted, skipping: %s", payload.EntryID)
		return nil
	}
	if entry.ParseStatus == types.ParseStatusCancelled {
		logger.Infof(ctx, "ProcessManualUpdate: cancelled by user, skipping: %s", payload.EntryID)
		return nil
	}

	library, err := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	if err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to get library: %v", err)
		entry.ParseStatus = "failed"
		entry.ErrorMessage = fmt.Sprintf("failed to get library: %v", err)
		entry.UpdatedAt = time.Now()
		s.repo.UpdateEntry(ctx, entry)
		return nil
	}

	// Re-check abort status right before marking processing — see the same
	// note in ProcessDocument for the cancel race this guards.
	if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
		logger.Infof(ctx, "ProcessManualUpdate: entry aborted (%s), skipping: %s", status, entry.ID)
		return nil
	}
	// Update status to processing
	markEntryProcessing(entry, time.Now())
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		logger.Errorf(ctx, "ProcessManualUpdate: failed to update status to processing: %v", err)
		return nil
	}

	// Allocate an attempt and pass it through article processing and indexing
	// so all manual processing stages belong to this execution.
	attempt := 0
	if root, n, err := s.tracker().OpenAttempt(ctx, entry.ID); err == nil && root != nil {
		attempt = n
	} else if err != nil {
		logger.Warnf(ctx, "ProcessManualUpdate: OpenAttempt failed for %s: %v", entry.ID, err)
	}
	ctx = withAttempt(ctx, attempt)

	// Cleanup old resources (indexes and chunks) for update operations
	if payload.NeedCleanup {
		if err := s.cleanupEntryResources(ctx, entry); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"entry_id": payload.EntryID,
			})
			entry.ParseStatus = "failed"
			entry.ErrorMessage = fmt.Sprintf("failed to cleanup old resources: %v", err)
			entry.UpdatedAt = time.Now()
			s.repo.UpdateEntry(ctx, entry)
			return nil
		}
	}

	// Run manual processing (image resolution + chunking + embedding) synchronously within the worker
	return s.triggerManualProcessing(ctx, library, entry, payload.Content, true)
}

// ProcessDocument handles PostgreSQL queue document processing tasks
func (s *entryService) ProcessDocument(ctx context.Context, t *taskqueue.Task) (returnErr error) {
	var payload types.DocumentProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "failed to unmarshal document process task payload: %v", err)
		return nil
	}

	ctx = logger.WithRequestID(ctx, payload.RequestId)
	ctx = logger.WithField(ctx, "document_process", payload.EntryID)
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	if payload.Language != "" {
		ctx = context.WithValue(ctx, types.LanguageContextKey, payload.Language)
	}

	// 获取任务重试信息，用于判断是否是最后一次重试

	spaceInfo, err := s.spaceRepo.GetSpaceByID(ctx, payload.SpaceID)
	if err != nil {
		logger.Errorf(ctx, "failed to get space: %v", err)
		return nil
	}
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, spaceInfo)

	retryCount, _ := taskqueue.GetRetryCount(ctx)
	maxRetry, _ := taskqueue.GetMaxRetry(ctx)
	logger.Infof(ctx, "Processing document task: entry_id=%s, file_path=%s, retry=%d/%d",
		payload.EntryID, payload.FilePath, retryCount, maxRetry)

	// 幂等性检查：获取entry记录
	entry, err := s.repo.GetEntryByID(ctx, payload.SpaceID, payload.EntryID)
	if err != nil {
		logger.Errorf(ctx, "failed to get entry: %v", err)
		return nil
	}

	if entry == nil {
		return nil
	}

	if intake, _ := entry.IntakeMetadata(); intake != nil {
		if intake.Decision == "rejected" || intake.Decision == "error" {
			return nil
		}
		defer func() {
			if types.IsChatGPTDeferred(returnErr) {
				s.markArticleProcessingFailed(ctx, entry, returnErr)
				return
			}
			if returnErr != nil && entry.ErrorMessage == "" {
				entry.ErrorMessage = returnErr.Error()
			}
			if returnErr != nil || entry.ParseStatus == types.ParseStatusFailed {
				s.failIntake(ctx, entry)
			}
		}()
	}

	// 检查是否正在删除 / 已被用户取消 - 如果是则直接退出
	if entry.ParseStatus == types.ParseStatusDeleting {
		logger.Infof(ctx, "Entry is being deleted, aborting processing: %s", payload.EntryID)
		return nil
	}
	if entry.ParseStatus == types.ParseStatusCancelled {
		logger.Infof(ctx, "Entry cancelled by user, aborting processing: %s", payload.EntryID)
		return nil
	}

	// 检查任务状态 - 幂等性处理
	if entry.ParseStatus == types.ParseStatusCompleted {
		logger.Infof(ctx, "Document already completed, skipping: %s", payload.EntryID)
		return nil // 幂等：已完成的任务直接返回
	}

	if payload.URL != "" || payload.FileURL != "" {
		body, hasBody, err := s.reprocessStoredBody(ctx, entry)
		if err != nil {
			return err
		}
		if !hasBody {
			_, err := s.markReprocessContentMissing(ctx, entry)
			return err
		}
		payload.URL, payload.FileURL = "", ""
		payload.Passages = []string{body}
	}

	if entry.ParseStatus == types.ParseStatusFailed {
		// 检查是否可恢复（例如：超时、临时错误等）
		// 对于不可恢复的错误，直接返回
		logger.Warnf(
			ctx,
			"Document processing previously failed: %s, error: %s",
			payload.EntryID,
			entry.ErrorMessage,
		)
		// 这里可以根据错误类型判断是否可恢复，暂时允许重试
	}

	// 检查是否有部分处理（有chunks但状态不是completed）
	if entry.ParseStatus != "completed" && entry.ParseStatus != "pending" &&
		entry.ParseStatus != "processing" {
		// 状态异常，记录日志但继续处理
		logger.Warnf(ctx, "Unexpected parse status: %s for entry: %s", entry.ParseStatus, payload.EntryID)
	}

	// 获取库信息
	library, err := s.libraryService.GetLibraryByID(ctx, entry.LibraryID)
	if err != nil {
		logger.Errorf(ctx, "failed to get library: %v", err)
		entry.ParseStatus = "failed"
		entry.ErrorMessage = fmt.Sprintf("failed to get library: %v", err)
		entry.UpdatedAt = time.Now()
		s.repo.UpdateEntry(ctx, entry)
		return nil
	}

	processOverrides, _ := entry.ProcessOverrides()
	eff := ResolveProcessConfig(library, processOverrides)

	// Re-check abort status right before flipping to "processing" — closes
	// the race where the user cancels between the entry guard above and
	// this write (otherwise the worker would overwrite cancelled→processing
	// and downstream checkpoints would treat the run as live).
	if aborted, status := s.isEntryAborted(ctx, entry.SpaceID, entry.ID); aborted {
		logger.Infof(ctx, "Entry aborted (%s) before marking processing: %s", status, entry.ID)
		return nil
	}
	markEntryProcessing(entry, time.Now())
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		logger.Errorf(ctx, "failed to update entry status to processing: %v", err)
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
		if root, n, err := s.tracker().OpenAttempt(ctx, entry.ID); err == nil && root != nil {
			attempt = n
		}
	}
	ctx = withAttempt(ctx, attempt)

	// Parse, resolve body images, review intake, then process the article once.
	var convertResult *types.ReadResult

	if len(payload.Passages) > 0 {
		convertResult = &types.ReadResult{MarkdownContent: strings.Join(payload.Passages, "\n\n")}
	} else {
		// File import
		return werrors.NewBadRequestError("content is required; convert the source to Markdown before ingestion")
	}

	// Step 2: Store images and update markdown references
	var storedImages []docparser.StoredImage

	if s.imageResolver != nil && convertResult != nil {
		fileSvc := s.resolveFileService(ctx, library)
		spaceID, _ := ctx.Value(types.SpaceIDContextKey).(uint64)
		updatedMarkdown, images, resolveErr := s.imageResolver.ResolveAndStore(ctx, convertResult, fileSvc, spaceID)
		if err := s.recordIntakeImages(ctx, entry, images); err != nil {
			return err
		}
		if resolveErr != nil {
			logger.Warnf(ctx, "Image resolution partially failed: %v", resolveErr)
			if intake, _ := entry.IntakeMetadata(); intake != nil {
				return fmt.Errorf("resolve intake images: %w", resolveErr)
			}
		}
		if updatedMarkdown != "" {
			convertResult.MarkdownContent = updatedMarkdown
		}
		storedImages = images

		if err := s.recordMissingIngestImages(ctx, entry, convertResult.MarkdownContent); err != nil {
			return err
		}
		if intake, _ := entry.IntakeMetadata(); intake != nil && s.hasUnstoredArticleImages(entry, convertResult.MarkdownContent) {
			return fmt.Errorf("intake has unstored content images")
		}

		logger.Infof(ctx, "Resolved %d total images for entry %s", len(storedImages), entry.ID)
	}

	// Step 3: Split into chunks using Go chunker. Browser textareas normalize
	// pasted content to LF, so normalize uploaded source text before calculating
	// chunk boundaries as well.
	sanitizeReadResult(convertResult)
	convertResult.MarkdownContent = chunker.NormalizeLineEndings(convertResult.MarkdownContent)
	if err := hydrateMessageSource(entry, convertResult.Metadata); err != nil {
		return err
	}
	if intake, _ := entry.IntakeMetadata(); intake != nil && intake.Decision != "approved" {
		existing, err := s.checkMessageDuplicate(ctx, entry, convertResult.MarkdownContent)
		if err != nil {
			return err
		}
		if existing != nil {
			return s.stopDuplicateProcessing(ctx, entry)
		}
	}
	if intake, _ := entry.IntakeMetadata(); intake != nil {
		if s.hasUnstoredArticleImages(entry, convertResult.MarkdownContent) {
			return fmt.Errorf("intake has unstored content images")
		}
		if err := s.saveIntakeOriginal(ctx, library, entry, convertResult.MarkdownContent, convertResult.Metadata); err != nil {
			return err
		}
	}

	if intake, _ := entry.IntakeMetadata(); intake != nil {
		rejected, reviewErr := s.reviewIntake(ctx, library, entry, convertResult.MarkdownContent)
		if reviewErr != nil {
			return reviewErr
		}
		if rejected {
			return nil
		}
	}
	if err := s.processArticleBody(ctx, library, entry, convertResult.MarkdownContent, storedImages, eff); err != nil {
		s.markArticleProcessingFailed(ctx, entry, err)
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

// convert parses only files already present in local storage.
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

// failEntry marks entry as failed (only on last retry) and returns an error.
func (s *entryService) failEntry(
	ctx context.Context,
	entry *types.Entry,
	isLastRetry bool,
	format string,
	args ...interface{},
) (*types.ReadResult, error) {
	errMsg := fmt.Sprintf(format, args...)
	if isLastRetry {
		entry.ParseStatus = "failed"
		entry.ErrorMessage = errMsg
		entry.UpdatedAt = time.Now()
		s.repo.UpdateEntry(ctx, entry)
	}
	return nil, fmt.Errorf(format, args...)
}

func (s *entryService) ProcessEntryListReparse(ctx context.Context, t *taskqueue.Task) error {
	var payload types.EntryListReparsePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal entry list reparse payload: %v", err)
		return err
	}
	ctx = payload.Initiator.Apply(ctx)

	logger.Infof(ctx, "Processing entry list reparse task for %d entry items", len(payload.EntryIDs))

	space, err := s.spaceRepo.GetSpaceByID(ctx, payload.SpaceID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get space %d: %v", payload.SpaceID, err)
		return err
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)

	outcome, err := runEntryListReparseSubmissions(payload.EntryIDs, func(id string) error {
		_, err := s.ReparseEntry(ctx, id, payload.ProcessConfig)
		return err
	})
	logger.Infof(ctx, "Entry list reparse task finished: %d submitted, %d failed",
		outcome.Submitted, outcome.Failed)
	return err
}

type entryListReparseOutcome struct {
	Submitted int
	Failed    int
}

// runEntryListReparseSubmissions attempts every item so one bad document
// cannot block the remainder of the batch. A partial failure is non-retryable:
// retrying the wrapper task would destructively reparse items that were already
// submitted successfully. Failed rows remain selectable for an explicit retry.
func runEntryListReparseSubmissions(
	ids []string,
	submit func(string) error,
) (entryListReparseOutcome, error) {
	var outcome entryListReparseOutcome
	failures := make([]error, 0)
	for _, id := range ids {
		if err := submit(id); err != nil {
			failures = append(failures, fmt.Errorf("entry %s: %w", secutils.SanitizeForLog(id), err))
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
