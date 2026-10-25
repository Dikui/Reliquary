package handler

import (
	"net/http"
	werrors "reliquary/internal/errors"

	"github.com/gin-gonic/gin"
)

// Candidates lists names awaiting or retaining a review decision.
func (h *EntityHandler) Candidates(c *gin.Context) {
	q, err := entityListQuery(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items, err := h.service.ListCandidates(c.Request.Context(), c.DefaultQuery("status", "pending"), q.Limit, q.Offset)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

// CandidateDetail exposes the recorded fact endpoints for a candidate.
func (h *EntityHandler) CandidateDetail(c *gin.Context) {
	q, err := entityListQuery(c)
	if err != nil {
		_ = c.Error(err)
		return
	}
	item, err := h.service.CandidateDetail(c.Request.Context(), c.Param("candidate_id"), q.Limit, q.Offset)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// ReviewCandidate approves, aliases or ignores a candidate.
func (h *EntityHandler) ReviewCandidate(c *gin.Context) {
	var input struct {
		EntityID string `json:"entity_id" binding:"required"`
	}
	if c.Param("action") == "alias" {
		if err := c.ShouldBindJSON(&input); err != nil {
			_ = c.Error(werrors.NewBadRequestError("entity_id required"))
			return
		}
	}
	item, err := h.service.ReviewCandidate(
		c.Request.Context(),
		c.Param("candidate_id"),
		c.Param("action"),
		input.EntityID,
	)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
