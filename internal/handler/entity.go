package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type EntityHandler struct {
	service   *service.EntityService
	rebuilder *service.EntityNameEmbeddingRebuilder
}

func NewEntityHandler(s *service.EntityService) *EntityHandler { return &EntityHandler{service: s} }
func (h *EntityHandler) SetNameEmbeddingRebuilder(r *service.EntityNameEmbeddingRebuilder) {
	h.rebuilder = r
}
func entityListQuery(c *gin.Context) (types.EntityQuery, error) {
	q := types.EntityQuery{LibraryID: c.Query("library_id"), Entity: c.Query("entity"), EntityType: c.Query("entity_type")}
	q.Kind = c.Query("kind")
	if raw, ok := c.GetQuery("include_archived"); ok {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return q, werrors.NewBadRequestError("include_archived must be a boolean")
		}
		q.IncludeArchived = value
	}
	var err error
	q.Limit, err = strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || q.Limit < 1 || q.Limit > 100 {
		return q, werrors.NewBadRequestError("limit must be 1-100")
	}
	q.Offset, err = strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || q.Offset < 0 {
		return q, werrors.NewBadRequestError("invalid offset")
	}
	return q, nil
}
func (h *EntityHandler) Entities(c *gin.Context) {
	q, err := entityListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListEntities(c.Request.Context(), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *EntityHandler) EntityDetail(c *gin.Context) {
	item, err := h.service.EntityDetail(c.Request.Context(), c.Param("entity_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *EntityHandler) MergeRecords(c *gin.Context) {
	items, err := h.service.ListMergeRecords(c.Request.Context(), c.Param("entity_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *EntityHandler) MergeEntities(c *gin.Context) {
	var input struct {
		Keep  string `json:"keep_entity_id" binding:"required"`
		Merge string `json:"merge_entity_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("two entity IDs required"))
		return
	}
	if err := h.service.MergeEntities(c.Request.Context(), input.Keep, input.Merge); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *EntityHandler) UpdateEntityDescription(c *gin.Context) {
	var input struct {
		Description *string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("description is required"))
		return
	}
	item, err := h.service.UpdateEntityDescription(c.Request.Context(), c.Param("entity_id"), *input.Description)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *EntityHandler) RenameEntity(c *gin.Context) {
	var input struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("entity name is required"))
		return
	}
	item, err := h.service.RenameEntity(c.Request.Context(), c.Param("entity_id"), input.Name)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *EntityHandler) MergeSuggestions(c *gin.Context) {
	q, err := entityListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListMergeSuggestions(c.Request.Context(), q, c.Query("status"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *EntityHandler) AcceptMergeSuggestion(c *gin.Context) { h.decideMergeSuggestion(c, true) }
func (h *EntityHandler) RejectMergeSuggestion(c *gin.Context) { h.decideMergeSuggestion(c, false) }
func (h *EntityHandler) decideMergeSuggestion(c *gin.Context, accept bool) {
	item, err := h.service.DecideMergeSuggestion(c.Request.Context(), c.Param("suggestion_id"), accept)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *EntityHandler) NameEmbeddingRebuildStatus(c *gin.Context) {
	if h.rebuilder == nil {
		c.Error(werrors.NewInternalServerError("name embedding rebuilder unavailable"))
		return
	}
	item, err := h.rebuilder.Status(c.Request.Context(), types.MustSpaceIDFromContext(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *EntityHandler) RebuildNameEmbeddings(c *gin.Context) {
	if h.rebuilder == nil {
		c.Error(werrors.NewInternalServerError("name embedding rebuilder unavailable"))
		return
	}
	ctx := c.Request.Context()
	if err := h.rebuilder.MarkAndEnqueue(ctx, types.MustSpaceIDFromContext(ctx), "administrator"); err != nil {
		c.Error(err)
		return
	}
	h.NameEmbeddingRebuildStatus(c)
}

func (h *EntityHandler) Relations(c *gin.Context) {
	items, err := h.service.ListRelations(c.Request.Context(), c.Param("entity_id"), c.Query("library_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *EntityHandler) DeleteRelation(c *gin.Context) {
	version, err := strconv.ParseInt(c.DefaultQuery("version", "0"), 10, 64)
	if err != nil {
		c.Error(werrors.NewBadRequestError("invalid relationship version"))
		return
	}
	if err := h.service.DeleteRelation(c.Request.Context(), c.Param("relation_id"), version); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *EntityHandler) MergeRelationTypes(c *gin.Context) {
	var input struct {
		From string `json:"from_type" binding:"required"`
		To   string `json:"to_type" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError(err.Error()))
		return
	}
	if err := h.service.MergeRelationTypes(c.Request.Context(), c.Param("id"), input.From, input.To); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
