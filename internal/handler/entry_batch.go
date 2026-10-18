package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	"reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntryHandler) entryBatches(c *gin.Context) (service.EntryBatchService, context.Context, bool) {
	svc, ok := h.kgService.(service.EntryBatchService)
	if !ok {
		c.Error(errors.NewInternalServerError("batch service unavailable"))
		return nil, nil, false
	}
	ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, c.GetUint64(types.SpaceIDContextKey.String()))
	return svc, ctx, true
}
func (h *EntryHandler) PreviewEntryBatch(c *gin.Context) {
	svc, ctx, ok := h.entryBatches(c)
	if !ok {
		return
	}
	var req types.EntryBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid batch request"))
		return
	}
	out, err := svc.PreviewEntryBatch(ctx, req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
func (h *EntryHandler) SubmitEntryBatch(c *gin.Context) {
	svc, ctx, ok := h.entryBatches(c)
	if !ok {
		return
	}
	var req types.EntryBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("invalid batch request"))
		return
	}
	out, err := svc.SubmitEntryBatch(ctx, req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
func (h *EntryHandler) GetEntryBatchResult(c *gin.Context) {
	svc, ctx, ok := h.entryBatches(c)
	if !ok {
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		c.Error(errors.NewBadRequestError("invalid page"))
		return
	}
	size, err := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || size < 1 || size > 100 {
		c.Error(errors.NewBadRequestError("invalid page_size"))
		return
	}
	out, err := svc.GetEntryBatchResult(ctx, c.Param("batch_id"), page, size)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
func (h *EntryHandler) RetryEntryBatch(c *gin.Context) {
	svc, ctx, ok := h.entryBatches(c)
	if !ok {
		return
	}
	out, err := svc.RetryEntryBatch(ctx, c.Param("batch_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
