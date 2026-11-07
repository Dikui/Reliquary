package handler

import (
	"net/http"
	"strconv"

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
