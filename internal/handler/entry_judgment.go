package handler

import (
	"context"
	"errors"
	"net/http"
	"reliquary/internal/types"
	"strconv"
	"time"

	werrors "reliquary/internal/errors"

	"github.com/gin-gonic/gin"
)

func (h *EntryHandler) ListPending(c *gin.Context) {
	if !h.applyLibraryReadScope(c) {
		return
	}
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

func (h *EntryHandler) SetStarred(c *gin.Context) {
	var input struct {
		Starred *bool `json:"starred"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Starred == nil {
		c.Error(werrors.NewBadRequestError("starred is required"))
		return
	}
	editor, ok := h.kgService.(interface {
		SetStarred(context.Context, string, bool) (*types.Entry, error)
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

// These user decisions commit synchronously. Keep the old article and require
// an explicit retry after recovery, rather than applying a stale choice later.
func writeChatGPTSynchronousPause(c *gin.Context, err error) bool {
	var paused *types.ChatGPTDeferredError
	if !errors.As(err, &paused) {
		return false
	}
	seconds := max(int((time.Until(paused.RetryAt)+time.Second-1)/time.Second), 1)
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": gin.H{
		"code":    "chatgpt_generation_paused",
		"message": "ChatGPT generation is paused. The article is unchanged. Restore availability in model settings, then retry this operation.",
		"details": gin.H{"reason": paused.Reason, "retry_at": paused.RetryAt, "manual_retry_required": true},
	}})
	return true
}
