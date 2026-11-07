package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntityHandler) SubmitShelfChangeset(c *gin.Context) {
	var input types.ShelfChangesetInput
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.SubmitShelfChangeset(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfChangeset(c *gin.Context) {
	result, err := h.service.ShelfChangeset(c.Request.Context(), c.Param("id"), c.Param("changeset_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfChangesets(c *gin.Context) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e1 != nil || e2 != nil {
		c.Error(apperrors.NewBadRequestError("invalid changeset pagination"))
		return
	}
	result, err := h.service.ShelfChangesets(c.Request.Context(), c.Param("id"), c.Query("status"), page, size)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ReviewShelfChangeset(c *gin.Context) {
	var input types.ShelfChangesetReview
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.ReviewShelfChangeset(c.Request.Context(), c.Param("id"), c.Param("changeset_id"), input, c.Request.Method == http.MethodPost)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfCertifications(c *gin.Context) {
	at := time.Now()
	if raw, ok := c.GetQuery("as_of"); ok {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || parsed.IsZero() {
			c.Error(apperrors.NewBadRequestError("as_of must be an RFC3339 time"))
			return
		}
		at = parsed
	}
	result, err := h.service.ShelfCertifications(c.Request.Context(), c.Param("id"), c.Query("standard"), at)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfImportPlans(c *gin.Context) {
	result, err := h.service.ShelfImportPlans(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
