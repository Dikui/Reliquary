package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// knowledgeTagService implements KnowledgeTagService.
type knowledgeTagService struct {
	kbService interfaces.KnowledgeBaseService
	repo      interfaces.KnowledgeTagRepository
	audit     interfaces.AuditLogService
}

// NewKnowledgeTagService creates a new tag service.
func NewKnowledgeTagService(
	kbService interfaces.KnowledgeBaseService,
	repo interfaces.KnowledgeTagRepository,
	audit interfaces.AuditLogService,
) (interfaces.KnowledgeTagService, error) {
	return &knowledgeTagService{
		kbService: kbService,
		repo:      repo,
		audit:     audit,
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
		return nil, werrors.NewForbiddenError("无权访问该知识库")
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

func (s *knowledgeTagService) DeleteTag(ctx context.Context, id string) error {
	if id == "" {
		return werrors.NewBadRequestError("标签ID不能为空")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	tag, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, tag.KnowledgeBaseID)
	if err != nil {
		return err
	}
	canEdit := types.TenantRoleFromContext(ctx).HasPermission(types.TenantRoleContributor)
	if scope, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		canEdit = scope.AllowsKnowledgeBase(kb.ID) && (scope.FullAccess || scope.HasCapability(types.APIKeyCapabilityIngest))
	}
	if kb.TenantID != tenantID || !canEdit {
		return werrors.NewForbiddenError("编辑者以上角色可删除标签")
	}

	if err := s.repo.Delete(ctx, tenantID, id); err != nil {
		return err
	}
	recordKBActivity(ctx, s.audit, tenantID, kb.ID, types.AuditActionTagDeleted,
		"knowledge_tag", tag.ID, types.AuditOutcomeSuccess,
		map[string]any{"name": tag.Name, "kind": tag.Kind, "relations_removed": true})
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
