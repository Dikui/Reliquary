package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

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
