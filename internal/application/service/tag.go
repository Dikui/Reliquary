package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"reliquary/internal/application/service/retriever"
	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/tracing/langfuse"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// knowledgeTagService implements KnowledgeTagService.
type knowledgeTagService struct {
	kbService      interfaces.KnowledgeBaseService
	repo           interfaces.KnowledgeTagRepository
	chunkRepo      interfaces.ChunkRepository
	retrieveEngine interfaces.RetrieveEngineRegistry
	ownership      retriever.TenantStoreOwnership
	modelService   interfaces.ModelService
	task           interfaces.TaskEnqueuer
	kbShareService interfaces.KBShareService
	audit          interfaces.AuditLogService
}

// NewKnowledgeTagService creates a new tag service.
func NewKnowledgeTagService(
	kbService interfaces.KnowledgeBaseService,
	repo interfaces.KnowledgeTagRepository,
	chunkRepo interfaces.ChunkRepository,
	retrieveEngine interfaces.RetrieveEngineRegistry,
	ownership retriever.TenantStoreOwnership,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
	kbShareService interfaces.KBShareService,
	audit interfaces.AuditLogService,
) (interfaces.KnowledgeTagService, error) {
	return &knowledgeTagService{
		kbService:      kbService,
		repo:           repo,
		chunkRepo:      chunkRepo,
		retrieveEngine: retrieveEngine,
		ownership:      ownership,
		modelService:   modelService,
		task:           task,
		kbShareService: kbShareService,
		audit:          audit,
	}, nil
}

// ListTags lists all tags for a knowledge base with usage stats.
func (s *knowledgeTagService) ListTags(
	ctx context.Context,
	kbID string,
	page *types.Pagination,
	keyword string,
) (*types.PageResult, error) {
	if kbID == "" {
		return nil, werrors.NewBadRequestError("知识库ID不能为空")
	}
	if page == nil {
		page = &types.Pagination{}
	}
	keyword = strings.TrimSpace(keyword)
	// Ensure KB exists
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}

	// Check access permission
	tenantID := types.MustTenantIDFromContext(ctx)
	if kb.TenantID != tenantID {
		// Get user ID from context
		userIDVal := ctx.Value(types.UserIDContextKey)
		if userIDVal == nil {
			return nil, werrors.NewForbiddenError("无权访问该知识库")
		}
		_ = userIDVal.(string)
		callerTenantRole := types.TenantRoleFromContext(ctx)

		// Check whether the caller's tenant has at least viewer permission via org sharing.
		hasPermission, err := s.kbShareService.HasTenantKBPermission(ctx, kbID, tenantID, callerTenantRole, types.OrgRoleViewer)
		if err != nil || !hasPermission {
			return nil, werrors.NewForbiddenError("无权访问该知识库")
		}
	}

	// Use kb's tenant ID for data access
	effectiveTenantID := kb.TenantID

	tags, total, err := s.repo.ListByKB(ctx, effectiveTenantID, kbID, page, keyword)
	if err != nil {
		return nil, err
	}

	if len(tags) == 0 {
		return types.NewPageResult(total, page, []*types.KnowledgeTagWithStats{}), nil
	}

	// Collect all tag IDs for batch query
	tagIDs := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != nil {
			tagIDs = append(tagIDs, tag.ID)
		}
	}

	// Batch query all reference counts in 2 SQL queries instead of 2*N
	countsMap, err := s.repo.BatchCountReferences(ctx, effectiveTenantID, kbID, tagIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id": kbID,
		})
		return nil, err
	}

	results := make([]*types.KnowledgeTagWithStats, 0, len(tags))
	for _, tag := range tags {
		if tag == nil {
			continue
		}
		counts := countsMap[tag.ID]
		results = append(results, &types.KnowledgeTagWithStats{
			KnowledgeTag:   *tag,
			KnowledgeCount: counts.KnowledgeCount,
			ChunkCount:     counts.ChunkCount,
		})
	}

	return types.NewPageResult(total, page, results), nil
}

// CreateTag creates a new tag under a KB.
func (s *knowledgeTagService) CreateTag(
	ctx context.Context,
	kbID string,
	name string,
	color string,
	sortOrder int,
	kinds ...string,
) (*types.KnowledgeTag, error) {
	name = strings.TrimSpace(name)
	if kbID == "" || name == "" {
		return nil, werrors.NewBadRequestError("知识库ID和标签名称不能为空")
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	kind := ""
	if kb.Type == types.KnowledgeBaseTypeDocument {
		kind = types.TagKindTopic
		if len(kinds) > 0 && kinds[0] != "" {
			kind = kinds[0]
		}
		if !validDocumentTag(kind, name) {
			return nil, werrors.NewBadRequestError("标签类别或名称无效")
		}
	}

	// Check if tag with same name already exists
	existingTag, err := s.repo.GetByName(ctx, kb.TenantID, kbID, name, kind)
	if err == nil && existingTag != nil {
		return nil, werrors.NewConflictError("标签名称已存在")
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	now := time.Now()
	// "未分类" tag should have the lowest sort order to appear first
	if name == types.UntaggedTagName {
		sortOrder = -1
	}
	tag := &types.KnowledgeTag{
		ID:              uuid.New().String(),
		TenantID:        kb.TenantID,
		KnowledgeBaseID: kb.ID,
		Name:            name,
		Kind:            kind,
		Color:           strings.TrimSpace(color),
		SortOrder:       sortOrder,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.repo.Create(ctx, tag); err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, tag.TenantID, tag.KnowledgeBaseID, types.AuditActionTagCreated,
		"knowledge_tag", tag.ID, types.AuditOutcomeSuccess, map[string]any{"name": tag.Name, "kind": tag.Kind})
	return tag, nil
}

// UpdateTag updates tag basic information.
func (s *knowledgeTagService) UpdateTag(
	ctx context.Context,
	id string,
	name *string,
	color *string,
	sortOrder *int,
	kinds ...*string,
) (*types.KnowledgeTag, error) {
	if id == "" {
		return nil, werrors.NewBadRequestError("标签ID不能为空")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	tag, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	oldName, oldKind := tag.Name, tag.Kind
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, tag.KnowledgeBaseID)
	if err != nil {
		return nil, err
	}
	if len(kinds) > 0 && kinds[0] != nil {
		if kb.Type == types.KnowledgeBaseTypeDocument {
			tag.Kind = *kinds[0]
		}
	}

	if name != nil {
		newName := strings.TrimSpace(*name)
		if newName == "" {
			return nil, werrors.NewBadRequestError("标签名称不能为空")
		}
		tag.Name = newName
	}
	if tag.Kind != "" && !validDocumentTag(tag.Kind, tag.Name) {
		return nil, werrors.NewBadRequestError("标签类别或名称无效")
	}
	if color != nil {
		tag.Color = strings.TrimSpace(*color)
	}
	if sortOrder != nil {
		tag.SortOrder = *sortOrder
	}
	tag.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, tag); err != nil {
		return nil, err
	}
	recordKBActivity(ctx, s.audit, tag.TenantID, tag.KnowledgeBaseID, types.AuditActionTagUpdated,
		"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
		map[string]any{"old_name": oldName, "name": tag.Name, "old_kind": oldKind, "kind": tag.Kind})
	return tag, nil
}

func validDocumentTag(kind, name string) bool {
	switch kind {
	case types.TagKindNature:
		return types.ContentNatures[name]
	case types.TagKindTopic, types.TagKindObject, types.TagKindEvent:
		return strings.TrimSpace(name) != "" && len([]rune(name)) <= 128
	default:
		return false
	}
}

// DeleteTag removes document tags and associations without deleting documents.
// FAQ tags retain their existing force/contentOnly chunk semantics.
func (s *knowledgeTagService) DeleteTag(ctx context.Context, id string, force bool, contentOnly bool, excludeIDs []string) error {
	if id == "" {
		return werrors.NewBadRequestError("标签ID不能为空")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	tag, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}

	// Get KB info for embedding model
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, tag.KnowledgeBaseID)
	if err != nil {
		return err
	}
	if kb.Type == types.KnowledgeBaseTypeDocument {
		userID, ok := types.UserIDFromContext(ctx)
		if !ok || userID != kb.CreatorID {
			return werrors.NewForbiddenError("仅知识库创建者可确认删除标签")
		}
		if contentOnly || len(excludeIDs) > 0 {
			return werrors.NewBadRequestError("消息标签删除不能删除消息内容")
		}
		if err := s.repo.Delete(ctx, tenantID, id); err != nil {
			return err
		}
		recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionTagDeleted,
			"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
			map[string]any{"name": tag.Name, "kind": tag.Kind, "relations_removed": true})
		return nil
	}

	kCount, cCount, err := s.repo.CountReferences(ctx, tenantID, tag.KnowledgeBaseID, tag.ID)
	if err != nil {
		return err
	}

	// Get tenant info for effective engines
	tenantInfo, _ := types.TenantInfoFromContext(ctx)

	// Helper function to delete chunks and enqueue index deletion task
	deleteChunksAndEnqueueIndexDelete := func() error {
		// Delete chunks and get their IDs
		deletedIDs, err := s.chunkRepo.DeleteChunksByTagID(ctx, tenantID, tag.KnowledgeBaseID, tag.ID, excludeIDs)
		if err != nil {
			logger.Errorf(ctx, "Failed to delete chunks by tag ID %s: %v", tag.ID, err)
			return werrors.NewInternalServerError("删除标签下的数据失败")
		}

		// Enqueue async index deletion task for the deleted chunks
		if len(deletedIDs) > 0 {
			s.enqueueIndexDeleteTask(ctx, tenantID, kb.ID, kb.EmbeddingModelID, string(kb.Type), deletedIDs, tenantInfo.GetEffectiveEngines(), kb.VectorStoreID)
		}

		logger.Infof(ctx, "Deleted %d chunks under tag %s", len(deletedIDs), tag.ID)
		return nil
	}

	// contentOnly mode: only delete content, keep the tag
	if contentOnly {
		if cCount > 0 {
			if err := deleteChunksAndEnqueueIndexDelete(); err != nil {
				return err
			}
		}
		recordKBActivity(ctx, s.audit, tenantID, tag.KnowledgeBaseID, types.AuditActionTagUpdated,
			"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
			map[string]any{"name": tag.Name, "content_cleared": true, "excluded_count": len(excludeIDs)})
		return nil
	}

	if !force && (kCount > 0 || cCount > 0) {
		return werrors.NewBadRequestError("标签仍有知识或FAQ条目引用，无法删除")
	}

	// FAQ force mode deletes chunks under this tag first.
	if force {
		if cCount > 0 {
			if err := deleteChunksAndEnqueueIndexDelete(); err != nil {
				return err
			}
		}
	}

	// If there are excludeIDs, we cannot delete the tag itself as it still has content
	if len(excludeIDs) > 0 {
		recordKBActivity(ctx, s.audit, tenantID, tag.KnowledgeBaseID, types.AuditActionTagUpdated,
			"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
			map[string]any{"name": tag.Name, "content_cleared": true, "excluded_count": len(excludeIDs)})
		return nil
	}
	if err := s.repo.Delete(ctx, tenantID, id); err != nil {
		return err
	}
	recordKBActivity(ctx, s.audit, tenantID, tag.KnowledgeBaseID, types.AuditActionTagDeleted,
		"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
		map[string]any{"name": tag.Name, "force": force})
	return nil
}

// enqueueIndexDeleteTask enqueues an async task for index deletion (low priority).
//
// vectorStoreID is captured from the owning KB at enqueue time and snapshotted
// into the payload so the worker can route to the correct store even if the
// KB is deleted or rebound before the task runs.
func (s *knowledgeTagService) enqueueIndexDeleteTask(ctx context.Context,
	tenantID uint64, kbID, embeddingModelID, kbType string, chunkIDs []string,
	effectiveEngines []types.RetrieverEngineParams, vectorStoreID *string,
) {
	payload := types.IndexDeletePayload{
		TenantID:         tenantID,
		KnowledgeBaseID:  kbID,
		EmbeddingModelID: embeddingModelID,
		KBType:           kbType,
		ChunkIDs:         chunkIDs,
		EffectiveEngines: effectiveEngines,
		VectorStoreID:    vectorStoreID,
	}
	langfuse.InjectTracing(ctx, &payload)
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal index delete payload: %v", err)
		return
	}

	task := asynq.NewTask(types.TypeIndexDelete, payloadBytes,
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(10), asynq.Timeout(time.Hour))
	info, err := s.task.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue index delete task: %v", err)
		return
	}
	logger.Infof(ctx, "Enqueued index delete task: %s for %d chunks", info.ID, len(chunkIDs))
}

// ProcessIndexDelete handles async index deletion task
func (s *knowledgeTagService) ProcessIndexDelete(ctx context.Context, t *asynq.Task) error {
	var payload types.IndexDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal index delete payload: %v", err)
		return err
	}

	// Set tenant context for downstream services
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)

	logger.Infof(ctx, "Processing index delete task for %d chunks in KB %s", len(payload.ChunkIDs), payload.KnowledgeBaseID)

	// Create retrieve engine.
	// The factory verifies the payload's tenant owns the bound store, so a
	// tampered queue entry cannot reach a cross-tenant store.
	// Forbidden/NotFound are non-retryable — SkipRetry prevents burning the
	// full retry budget on an unrecoverable task.
	retrieveEngine, err := retriever.CreateRetrieveEngineFromPayload(
		ctx, s.retrieveEngine, s.ownership,
		payload.TenantID, payload.EffectiveEngines, payload.VectorStoreID,
	)
	if errors.Is(err, retriever.ErrVectorStoreForbidden) ||
		errors.Is(err, retriever.ErrVectorStoreNotFound) {
		logger.Errorf(ctx, "Index delete task aborted: %v (tenant=%d, kb=%s)", err, payload.TenantID, payload.KnowledgeBaseID)
		return asynq.SkipRetry
	}
	// ErrVectorStoreUnavailable deliberately falls through to the retry path
	// below: the store exists and its engine may build on a later attempt.
	if err != nil {
		logger.Warnf(ctx, "Failed to create retrieve engine for index cleanup: %v", err)
		return err
	}

	// Get embedding model dimensions
	embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, payload.EmbeddingModelID)
	if err != nil {
		logger.Warnf(ctx, "Failed to get embedding model for index cleanup: %v", err)
		return err
	}

	// Delete indices in batches to avoid overwhelming the backend
	const batchSize = 100
	chunkIDs := payload.ChunkIDs
	dimension := embeddingModel.GetDimensions()

	for i := 0; i < len(chunkIDs); i += batchSize {
		end := i + batchSize
		if end > len(chunkIDs) {
			end = len(chunkIDs)
		}
		batch := chunkIDs[i:end]

		if err := retrieveEngine.DeleteByChunkIDList(ctx, batch, dimension, payload.KBType); err != nil {
			logger.Warnf(ctx, "Failed to delete indices for chunks batch [%d-%d]: %v", i, end, err)
			return err
		}
		logger.Debugf(ctx, "Deleted indices batch [%d-%d] of %d chunks", i, end, len(chunkIDs))
	}

	logger.Infof(ctx, "Successfully deleted indices for %d chunks", len(payload.ChunkIDs))
	return nil
}

// FindOrCreateTagByName finds a tag by name or creates it if not exists.
func (s *knowledgeTagService) FindOrCreateTagByName(ctx context.Context, kbID string, name string) (*types.KnowledgeTag, error) {
	name = strings.TrimSpace(name)
	if kbID == "" || name == "" {
		return nil, werrors.NewBadRequestError("知识库ID和标签名称不能为空")
	}

	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}

	tenantID := kb.TenantID

	// 先尝试查找现有标签
	tag, err := s.repo.GetByName(ctx, tenantID, kbID, name)
	if err == nil {
		return tag, nil
	}

	// 如果不是 not found 错误，直接返回
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 创建新标签
	return s.CreateTag(ctx, kbID, name, "", 0)
}
