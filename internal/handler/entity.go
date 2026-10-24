package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"strconv"

	werrors "reliquary/internal/errors"

	"github.com/gin-gonic/gin"
)

type EntityHandler struct {
	service *service.EntityService
}

func NewEntityHandler(s *service.EntityService) *EntityHandler { return &EntityHandler{service: s} }
func entityListQuery(c *gin.Context) (types.EntityQuery, error) {
	q := types.EntityQuery{Entity: c.Query("entity"), EntityType: c.Query("entity_type")}
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

// SaveEntity handles entity creation and replacement.
func (h *EntityHandler) SaveEntity(c *gin.Context) {
	var input types.EntityInput
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("entity name and type required"))
		return
	}
	item, err := h.service.SaveEntity(c.Request.Context(), c.Param("entity_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	status := http.StatusOK
	if c.Param("entity_id") == "" {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "data": item})
}

// DeleteEntity removes an entity and its associations.
func (h *EntityHandler) DeleteEntity(c *gin.Context) {
	if err := h.service.DeleteEntity(c.Request.Context(), c.Param("entity_id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// SetEntryEntities stores a manual material association choice.
func (h *EntityHandler) SetEntryEntities(c *gin.Context) {
	var input struct {
		EntityIDs *[]string `json:"entity_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		_ = c.Error(werrors.NewBadRequestError("entity_ids array required"))
		return
	}
	item, err := h.service.SetEntryEntities(c.Request.Context(), c.Param("id"), *input.EntityIDs)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
