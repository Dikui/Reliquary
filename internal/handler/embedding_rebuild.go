package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type EmbeddingRebuildHandler struct{ rebuilder *service.EmbeddingRebuilder }

func NewEmbeddingRebuildHandler(rebuilder *service.EmbeddingRebuilder) *EmbeddingRebuildHandler {
	return &EmbeddingRebuildHandler{rebuilder: rebuilder}
}

func (h *EmbeddingRebuildHandler) Status(c *gin.Context) {
	status, err := h.rebuilder.Status(c.Request.Context(), types.MustTenantIDFromContext(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

func (h *EmbeddingRebuildHandler) Action(c *gin.Context) {
	var input struct {
		Action string `json:"action" binding:"required"`
		RunID  string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("embedding rebuild action is required"))
		return
	}
	ctx := c.Request.Context()
	tenantID := types.MustTenantIDFromContext(ctx)
	var err error
	switch input.Action {
	case "start":
		_, err = h.rebuilder.StartAndEnqueue(ctx, tenantID)
	case "pause":
		err = h.rebuilder.Pause(ctx, tenantID, input.RunID)
	case "resume", "retry_failed":
		if input.RunID == "" {
			c.Error(apperrors.NewBadRequestError("embedding rebuild run_id is required"))
			return
		}
		err = h.rebuilder.ContinueAndEnqueue(ctx, tenantID, input.RunID, input.Action == "retry_failed")
	default:
		c.Error(apperrors.NewBadRequestError("unsupported embedding rebuild action"))
		return
	}
	if err != nil {
		c.Error(err)
		return
	}
	h.Status(c)
}
