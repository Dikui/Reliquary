package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *KnowledgeHandler) ListPending(c *gin.Context) {
	page, size := 0, 0
	for key, target := range map[string]*int{"page": &page, "page_size": &size} {
		if raw := c.Query(key); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 {
				c.Error(werrors.NewBadRequestError(key + " must be positive"))
				return
			}
			*target = value
		}
	}
	lister, ok := h.kgService.(interface {
		ListPending(context.Context, int, int, ...string) (*types.PageResult, error)
	})
	if !ok {
		c.Error(werrors.NewInternalServerError("pending review unavailable"))
		return
	}
	result, err := lister.ListPending(c.Request.Context(), page, size, c.Query("kind"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result.Data, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

func (h *KnowledgeHandler) ResolveProcessingReview(c *gin.Context) {
	var input struct {
		Action            string `json:"action"`
		ContentType       string `json:"content_type"`
		ConfirmRemoveBody bool   `json:"confirm_remove_body"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid processing review request"))
		return
	}
	editor, ok := h.kgService.(interface {
		ResolveProcessingReview(context.Context, string, string, string, bool) (*types.Knowledge, error)
	})
	if !ok {
		c.Error(werrors.NewInternalServerError("processing review unavailable"))
		return
	}
	item, err := editor.ResolveProcessingReview(c.Request.Context(), c.Param("id"), input.Action, input.ContentType, input.ConfirmRemoveBody)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *KnowledgeHandler) SetStarred(c *gin.Context) {
	var input struct {
		Starred *bool `json:"starred"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Starred == nil {
		c.Error(werrors.NewBadRequestError("starred is required"))
		return
	}
	editor, ok := h.kgService.(interface {
		SetStarred(context.Context, string, bool) (*types.Knowledge, error)
	})
	if !ok {
		c.Error(werrors.NewInternalServerError("article editing unavailable"))
		return
	}
	item, err := editor.SetStarred(c.Request.Context(), c.Param("id"), *input.Starred)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *KnowledgeHandler) CorrectContentType(c *gin.Context) {
	var input struct {
		ContentType       string `json:"content_type"`
		ConfirmRemoveBody bool   `json:"confirm_remove_body"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("content_type is required"))
		return
	}
	editor, ok := h.kgService.(interface {
		CorrectContentType(context.Context, string, string, bool) (*types.Knowledge, error)
	})
	if !ok {
		c.Error(werrors.NewInternalServerError("article editing unavailable"))
		return
	}
	item, err := editor.CorrectContentType(c.Request.Context(), c.Param("id"), input.ContentType, input.ConfirmRemoveBody)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
