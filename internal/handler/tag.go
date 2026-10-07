package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

type TagHandler struct {
	tagService interfaces.KnowledgeTagService
	tagRepo    interfaces.KnowledgeTagRepository
	chunkRepo  interfaces.ChunkRepository
}

// NewTagHandler creates a new TagHandler.
func NewTagHandler(
	tagService interfaces.KnowledgeTagService,
	tagRepo interfaces.KnowledgeTagRepository,
	chunkRepo interfaces.ChunkRepository,
) *TagHandler {
	return &TagHandler{tagService: tagService, tagRepo: tagRepo, chunkRepo: chunkRepo}
}

func (h *TagHandler) resolveTagID(c *gin.Context) (string, error) {
	return h.resolveTagIDWithCtx(c, c.Request.Context())
}

// resolveTagIDWithCtx resolves tag_id using the given context for tenant.
func (h *TagHandler) resolveTagIDWithCtx(c *gin.Context, ctx context.Context) (string, error) {
	tagIDParam := secutils.SanitizeForLog(c.Param("tag_id"))

	if seqID, err := strconv.ParseInt(tagIDParam, 10, 64); err == nil {
		tenantID := types.MustTenantIDFromContext(ctx)
		tag, err := h.tagRepo.GetBySeqID(ctx, tenantID, seqID)
		if err != nil {
			return "", errors.NewNotFoundError("标签不存在")
		}
		return tag.ID, nil
	}
	return tagIDParam, nil
}

// ListTags godoc
// @Summary      获取标签列表
// @Description  获取知识库下的所有标签及统计信息
// @Tags         标签管理
// @Accept       json
// @Produce      json
// @Param        id         path      string  true   "知识库ID"
// @Param        page       query     int     false  "页码"
// @Param        page_size  query     int     false  "每页数量"
// @Param        keyword    query     string  false  "关键词搜索"
// @Success      200        {object}  map[string]interface{}  "标签列表"
// @Failure      400        {object}  errors.AppError         "请求参数错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/tags [get]
func (h *TagHandler) ListTags(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		logger.Error(ctx, "Failed to bind pagination query", err)
		c.Error(errors.NewBadRequestError("分页参数不合法").WithDetails(err.Error()))
		return
	}

	keyword := secutils.SanitizeForLog(c.Query("keyword"))

	tags, err := h.tagService.ListTags(ctx, kbID, &page, keyword)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tags,
	})
}

type createTagRequest struct {
	Name      string `json:"name"       binding:"required"`
	Kind      string `json:"kind"`
	Color     string `json:"color"`
	SortOrder int    `json:"sort_order"`
}

// CreateTag godoc
// @Summary      创建标签
// @Description  在知识库下创建新标签
// @Tags         标签管理
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "知识库ID"
// @Param        request  body      object{name=string,color=string,sort_order=int}  true  "标签信息"
// @Success      200      {object}  map[string]interface{}  "创建的标签"
// @Failure      400      {object}  errors.AppError         "请求参数错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/tags [post]
func (h *TagHandler) CreateTag(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	var req createTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind create tag payload", err)
		c.Error(errors.NewBadRequestError("请求参数不合法").WithDetails(err.Error()))
		return
	}

	tag, err := h.tagService.CreateTag(ctx, kbID,
		secutils.SanitizeForLog(req.Name), secutils.SanitizeForLog(req.Color), req.SortOrder, req.Kind)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id": kbID,
		})
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tag,
	})
}

type updateTagRequest struct {
	Name      *string `json:"name"`
	Kind      *string `json:"kind"`
	Color     *string `json:"color"`
	SortOrder *int    `json:"sort_order"`
}

// UpdateTag godoc
// @Summary      更新标签
// @Description  更新标签信息
// @Tags         标签管理
// @Accept       json
// @Produce      json
// @Param        id       path      string  true  "知识库ID"
// @Param        tag_id   path      string  true  "标签ID (UUID或seq_id)"
// @Param        request  body      object  true  "标签更新信息"
// @Success      200      {object}  map[string]interface{}  "更新后的标签"
// @Failure      400      {object}  errors.AppError         "请求参数错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/tags/{tag_id} [put]
func (h *TagHandler) UpdateTag(c *gin.Context) {
	ctx := c.Request.Context()

	tagID, err := h.resolveTagID(c)
	if err != nil {
		c.Error(err)
		return
	}

	var req updateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to bind update tag payload", err)
		c.Error(errors.NewBadRequestError("请求参数不合法").WithDetails(err.Error()))
		return
	}

	tag, err := h.tagService.UpdateTag(ctx, tagID, req.Name, req.Color, req.SortOrder, req.Kind)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tag_id": tagID,
		})
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tag,
	})
}

// DeleteTag removes a team label and its associations while preserving documents.
func (h *TagHandler) DeleteTag(c *gin.Context) {
	ctx := c.Request.Context()
	tagID, err := h.resolveTagID(c)
	if err != nil {
		c.Error(err)
		return
	}
	if err := h.tagService.DeleteTag(ctx, tagID); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
