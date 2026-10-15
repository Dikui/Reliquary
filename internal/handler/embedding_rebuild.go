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
	status, err := h.rebuilder.Status(c.Request.Context(), types.MustSpaceIDFromContext(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

func (h *EmbeddingRebuildHandler) Action(c *gin.Context) {
	var input struct {
		Action  string `json:"action" binding:"required"`
		ModelID string `json:"model_id"`
		RunID   string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("embedding rebuild action is required"))
		return
	}
	ctx := c.Request.Context()
	spaceID := types.MustSpaceIDFromContext(ctx)
	var err error
	switch input.Action {
	case "start":
		if input.ModelID != "" {
			_, err = h.rebuilder.StartCandidateAndEnqueue(ctx, spaceID, input.ModelID)
		} else {
			_, err = h.rebuilder.StartAndEnqueue(ctx, spaceID)
		}
	case "cancel":
		err = h.rebuilder.CancelCandidate(ctx, spaceID, input.RunID)
	case "switch":
		err = h.rebuilder.SwitchCandidate(ctx, spaceID, input.RunID)
	case "delete_old":
		err = h.rebuilder.DeleteOldVectors(ctx, spaceID, input.RunID)
	case "pause":
		err = h.rebuilder.Pause(ctx, spaceID, input.RunID)
	case "resume", "retry_failed":
		if input.RunID == "" {
			c.Error(apperrors.NewBadRequestError("embedding rebuild run_id is required"))
			return
		}
		err = h.rebuilder.ContinueAndEnqueue(ctx, spaceID, input.RunID, input.Action == "retry_failed")
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
